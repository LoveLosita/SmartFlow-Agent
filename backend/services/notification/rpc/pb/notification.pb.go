package pb

import proto "github.com/golang/protobuf/proto"

var _ = proto.Marshal

const _ = proto.ProtoPackageIsVersion3

type GetFeishuWebhookRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *GetFeishuWebhookRequest) Reset()         { *m = GetFeishuWebhookRequest{} }
func (m *GetFeishuWebhookRequest) String() string { return proto.CompactTextString(m) }
func (*GetFeishuWebhookRequest) ProtoMessage()    {}

type SaveFeishuWebhookRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	Enabled              bool     `protobuf:"varint,2,opt,name=enabled,proto3" json:"enabled,omitempty"`
	WebhookUrl           string   `protobuf:"bytes,3,opt,name=webhook_url,json=webhookUrl,proto3" json:"webhook_url,omitempty"`
	AuthType             string   `protobuf:"bytes,4,opt,name=auth_type,json=authType,proto3" json:"auth_type,omitempty"`
	BearerToken          string   `protobuf:"bytes,5,opt,name=bearer_token,json=bearerToken,proto3" json:"bearer_token,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SaveFeishuWebhookRequest) Reset()         { *m = SaveFeishuWebhookRequest{} }
func (m *SaveFeishuWebhookRequest) String() string { return proto.CompactTextString(m) }
func (*SaveFeishuWebhookRequest) ProtoMessage()    {}

type DeleteFeishuWebhookRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *DeleteFeishuWebhookRequest) Reset()         { *m = DeleteFeishuWebhookRequest{} }
func (m *DeleteFeishuWebhookRequest) String() string { return proto.CompactTextString(m) }
func (*DeleteFeishuWebhookRequest) ProtoMessage()    {}

type TestFeishuWebhookRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *TestFeishuWebhookRequest) Reset()         { *m = TestFeishuWebhookRequest{} }
func (m *TestFeishuWebhookRequest) String() string { return proto.CompactTextString(m) }
func (*TestFeishuWebhookRequest) ProtoMessage()    {}

type StatusResponse struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *StatusResponse) Reset()         { *m = StatusResponse{} }
func (m *StatusResponse) String() string { return proto.CompactTextString(m) }
func (*StatusResponse) ProtoMessage()    {}

type ChannelResponse struct {
	Channel              string   `protobuf:"bytes,1,opt,name=channel,proto3" json:"channel,omitempty"`
	Enabled              bool     `protobuf:"varint,2,opt,name=enabled,proto3" json:"enabled,omitempty"`
	Configured           bool     `protobuf:"varint,3,opt,name=configured,proto3" json:"configured,omitempty"`
	WebhookUrlMask       string   `protobuf:"bytes,4,opt,name=webhook_url_mask,json=webhookUrlMask,proto3" json:"webhook_url_mask,omitempty"`
	AuthType             string   `protobuf:"bytes,5,opt,name=auth_type,json=authType,proto3" json:"auth_type,omitempty"`
	HasBearerToken       bool     `protobuf:"varint,6,opt,name=has_bearer_token,json=hasBearerToken,proto3" json:"has_bearer_token,omitempty"`
	LastTestStatus       string   `protobuf:"bytes,7,opt,name=last_test_status,json=lastTestStatus,proto3" json:"last_test_status,omitempty"`
	LastTestError        string   `protobuf:"bytes,8,opt,name=last_test_error,json=lastTestError,proto3" json:"last_test_error,omitempty"`
	LastTestAtUnixNano   int64    `protobuf:"varint,9,opt,name=last_test_at_unix_nano,json=lastTestAtUnixNano,proto3" json:"last_test_at_unix_nano,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ChannelResponse) Reset()         { *m = ChannelResponse{} }
func (m *ChannelResponse) String() string { return proto.CompactTextString(m) }
func (*ChannelResponse) ProtoMessage()    {}

type TestResult struct {
	Channel              *ChannelResponse `protobuf:"bytes,1,opt,name=channel,proto3" json:"channel,omitempty"`
	Status               string           `protobuf:"bytes,2,opt,name=status,proto3" json:"status,omitempty"`
	Outcome              string           `protobuf:"bytes,3,opt,name=outcome,proto3" json:"outcome,omitempty"`
	Message              string           `protobuf:"bytes,4,opt,name=message,proto3" json:"message,omitempty"`
	TraceId              string           `protobuf:"bytes,5,opt,name=trace_id,json=traceId,proto3" json:"trace_id,omitempty"`
	SentAtUnixNano       int64            `protobuf:"varint,6,opt,name=sent_at_unix_nano,json=sentAtUnixNano,proto3" json:"sent_at_unix_nano,omitempty"`
	Skipped              bool             `protobuf:"varint,7,opt,name=skipped,proto3" json:"skipped,omitempty"`
	Provider             string           `protobuf:"bytes,8,opt,name=provider,proto3" json:"provider,omitempty"`
	XXX_NoUnkeyedLiteral struct{}         `json:"-"`
	XXX_unrecognized     []byte           `json:"-"`
	XXX_sizecache        int32            `json:"-"`
}

func (m *TestResult) Reset()         { *m = TestResult{} }
func (m *TestResult) String() string { return proto.CompactTextString(m) }
func (*TestResult) ProtoMessage()    {}
