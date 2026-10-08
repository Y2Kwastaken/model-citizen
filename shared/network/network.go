// Package network holds the grpc definitions shared by the bot and the llm.
// The .proto files live in proto/ and the generated code in gen/.
//
// Regenerate after editing a .proto with, from shared/:
//
//	go generate ./network
package network

//go:generate go run github.com/bufbuild/buf/cmd/buf@v1.73.0 lint proto
//go:generate go run github.com/bufbuild/buf/cmd/buf@v1.73.0 generate
