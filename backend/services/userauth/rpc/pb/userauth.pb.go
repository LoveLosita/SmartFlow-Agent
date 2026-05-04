package pb

import proto "github.com/golang/protobuf/proto"

var _ = proto.Marshal

const _ = proto.ProtoPackageIsVersion3

type RegisterRequest struct {
	Username             string   `protobuf:"bytes,1,opt,name=username,proto3" json:"username,omitempty"`
	Password             string   `protobuf:"bytes,2,opt,name=password,proto3" json:"password,omitempty"`
	PhoneNumber          string   `protobuf:"bytes,3,opt,name=phone_number,json=phoneNumber,proto3" json:"phone_number,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *RegisterRequest) Reset()         { *m = RegisterRequest{} }
func (m *RegisterRequest) String() string { return proto.CompactTextString(m) }
func (*RegisterRequest) ProtoMessage()    {}

type RegisterResponse struct {
	Id                   uint64   `protobuf:"varint,1,opt,name=id,proto3" json:"id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *RegisterResponse) Reset()         { *m = RegisterResponse{} }
func (m *RegisterResponse) String() string { return proto.CompactTextString(m) }
func (*RegisterResponse) ProtoMessage()    {}

type LoginRequest struct {
	Username             string   `protobuf:"bytes,1,opt,name=username,proto3" json:"username,omitempty"`
	Password             string   `protobuf:"bytes,2,opt,name=password,proto3" json:"password,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *LoginRequest) Reset()         { *m = LoginRequest{} }
func (m *LoginRequest) String() string { return proto.CompactTextString(m) }
func (*LoginRequest) ProtoMessage()    {}

type TokensResponse struct {
	AccessToken          string   `protobuf:"bytes,1,opt,name=access_token,json=accessToken,proto3" json:"access_token,omitempty"`
	RefreshToken         string   `protobuf:"bytes,2,opt,name=refresh_token,json=refreshToken,proto3" json:"refresh_token,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *TokensResponse) Reset()         { *m = TokensResponse{} }
func (m *TokensResponse) String() string { return proto.CompactTextString(m) }
func (*TokensResponse) ProtoMessage()    {}

type RefreshTokenRequest struct {
	RefreshToken         string   `protobuf:"bytes,1,opt,name=refresh_token,json=refreshToken,proto3" json:"refresh_token,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *RefreshTokenRequest) Reset()         { *m = RefreshTokenRequest{} }
func (m *RefreshTokenRequest) String() string { return proto.CompactTextString(m) }
func (*RefreshTokenRequest) ProtoMessage()    {}

type LogoutRequest struct {
	AccessToken          string   `protobuf:"bytes,1,opt,name=access_token,json=accessToken,proto3" json:"access_token,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *LogoutRequest) Reset()         { *m = LogoutRequest{} }
func (m *LogoutRequest) String() string { return proto.CompactTextString(m) }
func (*LogoutRequest) ProtoMessage()    {}

type StatusResponse struct {
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *StatusResponse) Reset()         { *m = StatusResponse{} }
func (m *StatusResponse) String() string { return proto.CompactTextString(m) }
func (*StatusResponse) ProtoMessage()    {}

type ValidateAccessTokenRequest struct {
	AccessToken          string   `protobuf:"bytes,1,opt,name=access_token,json=accessToken,proto3" json:"access_token,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ValidateAccessTokenRequest) Reset()         { *m = ValidateAccessTokenRequest{} }
func (m *ValidateAccessTokenRequest) String() string { return proto.CompactTextString(m) }
func (*ValidateAccessTokenRequest) ProtoMessage()    {}

type ValidateAccessTokenResponse struct {
	Valid                bool     `protobuf:"varint,1,opt,name=valid,proto3" json:"valid,omitempty"`
	UserId               int64    `protobuf:"varint,2,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	TokenType            string   `protobuf:"bytes,3,opt,name=token_type,json=tokenType,proto3" json:"token_type,omitempty"`
	Jti                  string   `protobuf:"bytes,4,opt,name=jti,proto3" json:"jti,omitempty"`
	ExpiresAtUnixNano    int64    `protobuf:"varint,5,opt,name=expires_at_unix_nano,json=expiresAtUnixNano,proto3" json:"expires_at_unix_nano,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ValidateAccessTokenResponse) Reset()         { *m = ValidateAccessTokenResponse{} }
func (m *ValidateAccessTokenResponse) String() string { return proto.CompactTextString(m) }
func (*ValidateAccessTokenResponse) ProtoMessage()    {}

type CheckTokenQuotaRequest struct {
	UserId               int64    `protobuf:"varint,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *CheckTokenQuotaRequest) Reset()         { *m = CheckTokenQuotaRequest{} }
func (m *CheckTokenQuotaRequest) String() string { return proto.CompactTextString(m) }
func (*CheckTokenQuotaRequest) ProtoMessage()    {}

type AdjustTokenUsageRequest struct {
	EventId              string   `protobuf:"bytes,1,opt,name=event_id,json=eventId,proto3" json:"event_id,omitempty"`
	UserId               int64    `protobuf:"varint,2,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	TokenDelta           int64    `protobuf:"varint,3,opt,name=token_delta,json=tokenDelta,proto3" json:"token_delta,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *AdjustTokenUsageRequest) Reset()         { *m = AdjustTokenUsageRequest{} }
func (m *AdjustTokenUsageRequest) String() string { return proto.CompactTextString(m) }
func (*AdjustTokenUsageRequest) ProtoMessage()    {}

type CheckTokenQuotaResponse struct {
	Allowed              bool     `protobuf:"varint,1,opt,name=allowed,proto3" json:"allowed,omitempty"`
	TokenLimit           int64    `protobuf:"varint,2,opt,name=token_limit,json=tokenLimit,proto3" json:"token_limit,omitempty"`
	TokenUsage           int64    `protobuf:"varint,3,opt,name=token_usage,json=tokenUsage,proto3" json:"token_usage,omitempty"`
	LastResetAtUnixNano  int64    `protobuf:"varint,4,opt,name=last_reset_at_unix_nano,json=lastResetAtUnixNano,proto3" json:"last_reset_at_unix_nano,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *CheckTokenQuotaResponse) Reset()         { *m = CheckTokenQuotaResponse{} }
func (m *CheckTokenQuotaResponse) String() string { return proto.CompactTextString(m) }
func (*CheckTokenQuotaResponse) ProtoMessage()    {}
