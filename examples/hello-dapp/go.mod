module hello-dapp

go 1.24.2

toolchain go1.24.4

require github.com/soaringjerry/pcas v0.0.0

require (
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.24.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250324211829-b45e905df463 // indirect
	google.golang.org/grpc v1.73.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace github.com/soaringjerry/pcas => ../..
