package pb

import proto "github.com/golang/protobuf/proto"

var _ = proto.Marshal

const _ = proto.ProtoPackageIsVersion3

type JSONRequest struct {
	PayloadJson          []byte   `protobuf:"bytes,1,opt,name=payload_json,json=payloadJson,proto3" json:"payload_json,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *JSONRequest) Reset()         { *m = JSONRequest{} }
func (m *JSONRequest) String() string { return proto.CompactTextString(m) }
func (*JSONRequest) ProtoMessage()    {}

type JSONResponse struct {
	DataJson             []byte   `protobuf:"bytes,1,opt,name=data_json,json=dataJson,proto3" json:"data_json,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *JSONResponse) Reset()         { *m = JSONResponse{} }
func (m *JSONResponse) String() string { return proto.CompactTextString(m) }
func (*JSONResponse) ProtoMessage()    {}

type StatusResponse struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *StatusResponse) Reset()         { *m = StatusResponse{} }
func (m *StatusResponse) String() string { return proto.CompactTextString(m) }
func (*StatusResponse) ProtoMessage()    {}
