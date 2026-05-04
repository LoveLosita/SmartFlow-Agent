package pb

import proto "github.com/golang/protobuf/proto"

var _ = proto.Marshal

const _ = proto.ProtoPackageIsVersion3

type UserRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *UserRequest) Reset()         { *m = UserRequest{} }
func (m *UserRequest) String() string { return proto.CompactTextString(m) }
func (*UserRequest) ProtoMessage()    {}

type WeekRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	Week                 int64    `protobuf:"varint,2,opt,name=week,proto3" json:"week,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *WeekRequest) Reset()         { *m = WeekRequest{} }
func (m *WeekRequest) String() string { return proto.CompactTextString(m) }
func (*WeekRequest) ProtoMessage()    {}

type DeleteEventsRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	EventsJson           []byte   `protobuf:"bytes,2,opt,name=events_json,json=eventsJson,proto3" json:"events_json,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *DeleteEventsRequest) Reset()         { *m = DeleteEventsRequest{} }
func (m *DeleteEventsRequest) String() string { return proto.CompactTextString(m) }
func (*DeleteEventsRequest) ProtoMessage()    {}

type RecentCompletedRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	Index                int64    `protobuf:"varint,2,opt,name=index,proto3" json:"index,omitempty"`
	Limit                int64    `protobuf:"varint,3,opt,name=limit,proto3" json:"limit,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *RecentCompletedRequest) Reset()         { *m = RecentCompletedRequest{} }
func (m *RecentCompletedRequest) String() string { return proto.CompactTextString(m) }
func (*RecentCompletedRequest) ProtoMessage()    {}

type RevokeTaskItemRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	EventId              int64    `protobuf:"varint,2,opt,name=event_id,json=eventId,proto3" json:"event_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *RevokeTaskItemRequest) Reset()         { *m = RevokeTaskItemRequest{} }
func (m *RevokeTaskItemRequest) String() string { return proto.CompactTextString(m) }
func (*RevokeTaskItemRequest) ProtoMessage()    {}

type SmartPlanningRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	TaskClassId          int64    `protobuf:"varint,2,opt,name=task_class_id,json=taskClassId,proto3" json:"task_class_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SmartPlanningRequest) Reset()         { *m = SmartPlanningRequest{} }
func (m *SmartPlanningRequest) String() string { return proto.CompactTextString(m) }
func (*SmartPlanningRequest) ProtoMessage()    {}

type SmartPlanningMultiRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	TaskClassIds         []int64  `protobuf:"varint,2,rep,packed,name=task_class_ids,json=taskClassIds,proto3" json:"task_class_ids,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *SmartPlanningMultiRequest) Reset()         { *m = SmartPlanningMultiRequest{} }
func (m *SmartPlanningMultiRequest) String() string { return proto.CompactTextString(m) }
func (*SmartPlanningMultiRequest) ProtoMessage()    {}

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
