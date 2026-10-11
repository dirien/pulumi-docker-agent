// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package ociagent pushes, pulls and deletes docker-agent OCI artifacts with
// go-containerregistry. The artifacts are byte-compatible with
// `docker agent share push` (docker-agent v1.150.0):
//
//   - an OCI image manifest with artifactType `application/vnd.docker.agent.config.v1+json`
//   - a near-empty Docker image config (`application/vnd.docker.container.image.v1+json`)
//   - one layer (`application/vnd.docker.image.rootfs.diff.tar.gzip`) that is the gzip of
//     the raw agent YAML (not a tar archive)
//   - the `io.docker.agent.version` / `io.docker.cagent.version` annotations that
//     `docker agent share pull` requires
//
// Unlike the CLI, the `org.opencontainers.image.created` timestamp is supplied by the
// caller, so identical inputs produce an identical manifest digest.
package ociagent

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// Media types and annotation keys of a docker-agent artifact.
const (
	ArtifactType      = "application/vnd.docker.agent.config.v1+json"
	ManifestMediaType = types.OCIManifestSchema1
	ConfigMediaType   = types.DockerConfigJSON
	LayerMediaType    = types.DockerLayer

	AnnotationVersion       = "io.docker.agent.version"
	AnnotationLegacyVersion = "io.docker.cagent.version"
	AnnotationTags          = "io.docker.agent.tags"
	AnnotationCreated       = "org.opencontainers.image.created"
	AnnotationDescription   = "org.opencontainers.image.description"
	AnnotationAuthors       = "org.opencontainers.image.authors"
	AnnotationLicenses      = "org.opencontainers.image.licenses"
	AnnotationRevision      = "org.opencontainers.image.revision"

	// DefaultDescription is the description annotation `docker agent share push` writes
	// for a file called agent.yaml.
	DefaultDescription = "OCI artifact containing agent.yaml"
)

// DockerAgentVersion is written to the version annotations.
const DockerAgentVersion = validate.DockerAgentVersion

// ReservedAnnotations are set by the provider and cannot be overridden by users.
var ReservedAnnotations = []string{AnnotationVersion, AnnotationLegacyVersion, AnnotationCreated}

// Metadata is the `metadata` block of an agent YAML document.
type Metadata struct {
	Author      string   `yaml:"author"`
	License     string   `yaml:"license"`
	Description string   `yaml:"description"`
	Readme      string   `yaml:"readme"`
	Version     string   `yaml:"version"`
	Tags        []string `yaml:"tags"`
}

// ParseMetadata reads the `metadata` block of an agent YAML document. It is lenient: a
// document that cannot be parsed yields empty metadata.
func ParseMetadata(doc []byte) Metadata {
	var raw struct {
		Metadata Metadata `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(doc, &raw); err != nil {
		return Metadata{}
	}
	return raw.Metadata
}

// FormatCreated formats t the way `docker agent share push` does (RFC 3339, UTC, seconds).
func FormatCreated(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// Annotations returns the manifest annotations for an agent YAML document: the version
// annotations, the description, the created timestamp, the annotations derived from the
// YAML `metadata` block (author, license, version, tags) and finally extra, which may
// override everything except the reserved keys.
func Annotations(doc []byte, created string, extra map[string]string) map[string]string {
	ann := map[string]string{
		AnnotationVersion:       DockerAgentVersion,
		AnnotationLegacyVersion: DockerAgentVersion,
		AnnotationDescription:   DefaultDescription,
	}
	md := ParseMetadata(doc)
	if md.Author != "" {
		ann[AnnotationAuthors] = md.Author
	}
	if md.License != "" {
		ann[AnnotationLicenses] = md.License
	}
	if md.Version != "" {
		ann[AnnotationRevision] = md.Version
	}
	if len(md.Tags) > 0 {
		ann[AnnotationTags] = strings.Join(md.Tags, ",")
	}
	for k, v := range extra {
		if isReserved(k) {
			continue
		}
		ann[k] = v
	}
	if created != "" {
		ann[AnnotationCreated] = created
	}
	return ann
}

func isReserved(k string) bool {
	for _, r := range ReservedAnnotations {
		if k == r {
			return true
		}
	}
	return false
}

// Artifact is a fully built docker-agent artifact, ready to push.
type Artifact struct {
	YAML        []byte
	Layer       []byte // gzip of YAML
	Config      []byte
	Manifest    []byte
	Annotations map[string]string

	LayerDigest  v1.Hash
	DiffID       v1.Hash // sha256 of YAML
	ConfigDigest v1.Hash
	Digest       v1.Hash // manifest digest
}

// ContentSHA256 returns the hex sha256 of the YAML (the layer diff_id without prefix).
func (a *Artifact) ContentSHA256() string { return a.DiffID.Hex }

type descriptor struct {
	MediaType types.MediaType `json:"mediaType"`
	Size      int64           `json:"size"`
	Digest    v1.Hash         `json:"digest"`
}

// Build assembles the artifact for doc with the given manifest annotations. Identical
// inputs always produce identical bytes and digests.
func Build(doc []byte, annotations map[string]string) (*Artifact, error) {
	layer, err := Gzip(doc)
	if err != nil {
		return nil, err
	}
	a := &Artifact{
		YAML:        doc,
		Layer:       layer,
		Annotations: maps.Clone(annotations),
		LayerDigest: hashOf(layer),
		DiffID:      hashOf(doc),
	}
	// Byte-identical to the config go-containerregistry produces for empty.Image plus one
	// layer, which is what `docker agent share push` uploads.
	a.Config = fmt.Appendf(nil,
		`{"architecture":"","created":"0001-01-01T00:00:00Z","history":[{"created":"0001-01-01T00:00:00Z"}],`+
			`"os":"","rootfs":{"type":"layers","diff_ids":[%q]},"config":{}}`, a.DiffID.String())
	a.ConfigDigest = hashOf(a.Config)

	// encoding/json sorts map keys, which reproduces the CLI's manifest byte for byte.
	manifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     ManifestMediaType,
		"artifactType":  ArtifactType,
		"config":        descriptor{MediaType: ConfigMediaType, Size: int64(len(a.Config)), Digest: a.ConfigDigest},
		"layers":        []descriptor{{MediaType: LayerMediaType, Size: int64(len(a.Layer)), Digest: a.LayerDigest}},
	}
	if len(annotations) > 0 {
		manifest["annotations"] = annotations
	}
	a.Manifest, err = json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encoding manifest: %w", err)
	}
	a.Digest = hashOf(a.Manifest)
	return a, nil
}

// Gzip compresses b deterministically: no file name, no modification time, and the
// compression level go-containerregistry (and therefore docker-agent) uses.
func Gzip(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(b); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func hashOf(b []byte) v1.Hash {
	sum := sha256.Sum256(b)
	return v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum[:])}
}

// image adapts an Artifact to go-containerregistry's v1.Image for remote.Write.
func (a *Artifact) image() (v1.Image, error) {
	return partial.CompressedToImage(&compressedImage{a: a})
}

type compressedImage struct{ a *Artifact }

func (c *compressedImage) RawConfigFile() ([]byte, error)      { return c.a.Config, nil }
func (c *compressedImage) MediaType() (types.MediaType, error) { return ManifestMediaType, nil }
func (c *compressedImage) RawManifest() ([]byte, error)        { return c.a.Manifest, nil }

func (c *compressedImage) LayerByDigest(h v1.Hash) (partial.CompressedLayer, error) {
	switch h {
	case c.a.LayerDigest:
		return &blob{data: c.a.Layer, digest: h, mediaType: LayerMediaType}, nil
	case c.a.ConfigDigest:
		return &blob{data: c.a.Config, digest: h, mediaType: ConfigMediaType}, nil
	}
	return nil, fmt.Errorf("blob %s not found", h)
}

type blob struct {
	data      []byte
	digest    v1.Hash
	mediaType types.MediaType
}

func (b *blob) Digest() (v1.Hash, error)            { return b.digest, nil }
func (b *blob) Compressed() (io.ReadCloser, error)  { return io.NopCloser(bytes.NewReader(b.data)), nil }
func (b *blob) Size() (int64, error)                { return int64(len(b.data)), nil }
func (b *blob) MediaType() (types.MediaType, error) { return b.mediaType, nil }
