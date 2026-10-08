module github.com/Y2Kwastaken/model-citizen/llm

go 1.27.1

replace github.com/Y2Kwastaken/model-citizen/shared => ../shared/

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/Y2Kwastaken/model-citizen/shared v1.0.0
	github.com/openai/openai-go/v3 v3.57.0
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
)
