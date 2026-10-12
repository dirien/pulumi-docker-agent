// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	p "github.com/pulumi/pulumi-go-provider"
)

// userLogger is the part of p.Logger the provider reports to the user with.
type userLogger interface {
	Debug(msg string)
	Info(msg string)
	InfoStatus(msg string)
	Warning(msg string)
}

// loggerKey lets tests capture what the provider reports: a userLogger stored under it in the
// context replaces p.GetLogger.
type loggerKey struct{}

// userLog returns the logger for messages shown to the user.
func userLog(ctx context.Context) userLogger {
	if l, ok := ctx.Value(loggerKey{}).(userLogger); ok {
		return l
	}
	return p.GetLogger(ctx)
}
