These are the example configurations of github.com/docker/docker-agent v1.150.0 (examples/**/*.yaml and *.hcl,
with the Markdown files they read), copied by `make generate_config_types`. other-files.txt lists the
other example files. Do not edit them: TestRoundTripUpstreamExamples decodes each configuration
into the typed inputs, renders it again and compares what docker-agent loads, and
provider/internal/examplegen turns each one into the Pulumi programs in
examples/docker-agent-examples.
