package pb

import proto "github.com/golang/protobuf/proto"

var _ = proto.Marshal

const _ = proto.ProtoPackageIsVersion3

type ChatRequest struct {
	Message              string   `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
	Thinking             string   `protobuf:"bytes,2,opt,name=thinking,proto3" json:"thinking,omitempty"`
	Model                string   `protobuf:"bytes,3,opt,name=model,proto3" json:"model,omitempty"`
	UserId               int32    `protobuf:"varint,4,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	ConversationId       string   `protobuf:"bytes,5,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	ExtraJson            []byte   `protobuf:"bytes,6,opt,name=extra_json,json=extraJson,proto3" json:"extra_json,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ChatRequest) Reset()         { *m = ChatRequest{} }
func (m *ChatRequest) String() string { return proto.CompactTextString(m) }
func (*ChatRequest) ProtoMessage()    {}

type ChatChunk struct {
	Payload              string   `protobuf:"bytes,1,opt,name=payload,proto3" json:"payload,omitempty"`
	Done                 bool     `protobuf:"varint,2,opt,name=done,proto3" json:"done,omitempty"`
	ErrorJson            []byte   `protobuf:"bytes,3,opt,name=error_json,json=errorJson,proto3" json:"error_json,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ChatChunk) Reset()         { *m = ChatChunk{} }
func (m *ChatChunk) String() string { return proto.CompactTextString(m) }
func (*ChatChunk) ProtoMessage()    {}

type StatusResponse struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *StatusResponse) Reset()         { *m = StatusResponse{} }
func (m *StatusResponse) String() string { return proto.CompactTextString(m) }
func (*StatusResponse) ProtoMessage()    {}

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
