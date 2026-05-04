package pb

import proto "github.com/golang/protobuf/proto"

var _ = proto.Marshal

const _ = proto.ProtoPackageIsVersion3

type ActiveScheduleRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	TriggerType          string   `protobuf:"bytes,2,opt,name=trigger_type,json=triggerType,proto3" json:"trigger_type,omitempty"`
	TargetType           string   `protobuf:"bytes,3,opt,name=target_type,json=targetType,proto3" json:"target_type,omitempty"`
	TargetId             int64    `protobuf:"varint,4,opt,name=target_id,json=targetId,proto3" json:"target_id,omitempty"`
	FeedbackId           string   `protobuf:"bytes,5,opt,name=feedback_id,json=feedbackId,proto3" json:"feedback_id,omitempty"`
	IdempotencyKey       string   `protobuf:"bytes,6,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
	MockNowUnixNano      int64    `protobuf:"varint,7,opt,name=mock_now_unix_nano,json=mockNowUnixNano,proto3" json:"mock_now_unix_nano,omitempty"`
	PayloadJson          []byte   `protobuf:"bytes,8,opt,name=payload_json,json=payloadJson,proto3" json:"payload_json,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ActiveScheduleRequest) Reset()         { *m = ActiveScheduleRequest{} }
func (m *ActiveScheduleRequest) String() string { return proto.CompactTextString(m) }
func (*ActiveScheduleRequest) ProtoMessage()    {}

type GetPreviewRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	PreviewId            string   `protobuf:"bytes,2,opt,name=preview_id,json=previewId,proto3" json:"preview_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *GetPreviewRequest) Reset()         { *m = GetPreviewRequest{} }
func (m *GetPreviewRequest) String() string { return proto.CompactTextString(m) }
func (*GetPreviewRequest) ProtoMessage()    {}

type ConfirmPreviewRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	PreviewId            string   `protobuf:"bytes,2,opt,name=preview_id,json=previewId,proto3" json:"preview_id,omitempty"`
	CandidateId          string   `protobuf:"bytes,3,opt,name=candidate_id,json=candidateId,proto3" json:"candidate_id,omitempty"`
	Action               string   `protobuf:"bytes,4,opt,name=action,proto3" json:"action,omitempty"`
	EditedChangesJson    []byte   `protobuf:"bytes,5,opt,name=edited_changes_json,json=editedChangesJson,proto3" json:"edited_changes_json,omitempty"`
	IdempotencyKey       string   `protobuf:"bytes,6,opt,name=idempotency_key,json=idempotencyKey,proto3" json:"idempotency_key,omitempty"`
	RequestedAtUnixNano  int64    `protobuf:"varint,7,opt,name=requested_at_unix_nano,json=requestedAtUnixNano,proto3" json:"requested_at_unix_nano,omitempty"`
	TraceId              string   `protobuf:"bytes,8,opt,name=trace_id,json=traceId,proto3" json:"trace_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ConfirmPreviewRequest) Reset()         { *m = ConfirmPreviewRequest{} }
func (m *ConfirmPreviewRequest) String() string { return proto.CompactTextString(m) }
func (*ConfirmPreviewRequest) ProtoMessage()    {}

type JSONResponse struct {
	DataJson             []byte   `protobuf:"bytes,1,opt,name=data_json,json=dataJson,proto3" json:"data_json,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *JSONResponse) Reset()         { *m = JSONResponse{} }
func (m *JSONResponse) String() string { return proto.CompactTextString(m) }
func (*JSONResponse) ProtoMessage()    {}

type TriggerResponse struct {
	TriggerId            string   `protobuf:"bytes,1,opt,name=trigger_id,json=triggerId,proto3" json:"trigger_id,omitempty"`
	Status               string   `protobuf:"bytes,2,opt,name=status,proto3" json:"status,omitempty"`
	PreviewId            string   `protobuf:"bytes,3,opt,name=preview_id,json=previewId,proto3" json:"preview_id,omitempty"`
	HasPreviewId         bool     `protobuf:"varint,4,opt,name=has_preview_id,json=hasPreviewId,proto3" json:"has_preview_id,omitempty"`
	DedupeHit            bool     `protobuf:"varint,5,opt,name=dedupe_hit,json=dedupeHit,proto3" json:"dedupe_hit,omitempty"`
	TraceId              string   `protobuf:"bytes,6,opt,name=trace_id,json=traceId,proto3" json:"trace_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *TriggerResponse) Reset()         { *m = TriggerResponse{} }
func (m *TriggerResponse) String() string { return proto.CompactTextString(m) }
func (*TriggerResponse) ProtoMessage()    {}
