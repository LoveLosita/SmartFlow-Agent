package rpc

import (
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

const jsonCodecName = "smartflow-json"

type jsonCodec struct{}

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

func (jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return jsonCodecName
}

// JSONCodecDialOption 负责让 zrpc client 按 JSON 编解码本服务请求体。
func JSONCodecDialOption() grpc.DialOption {
	return grpc.WithDefaultCallOptions(grpc.ForceCodec(jsonCodec{}))
}

// JSONCodecServerOption 负责让 zrpc server 按 JSON 编解码本服务请求体。
func JSONCodecServerOption() grpc.ServerOption {
	return grpc.ForceServerCodec(jsonCodec{})
}
