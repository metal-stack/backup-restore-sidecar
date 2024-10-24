// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.35.1
// 	protoc        (unknown)
// source: v1/initializer.proto

package v1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type StatusResponse_InitializerStatus int32

const (
	StatusResponse_CHECKING  StatusResponse_InitializerStatus = 0
	StatusResponse_RESTORING StatusResponse_InitializerStatus = 1
	StatusResponse_DONE      StatusResponse_InitializerStatus = 2
	StatusResponse_UPGRADING StatusResponse_InitializerStatus = 3
)

// Enum value maps for StatusResponse_InitializerStatus.
var (
	StatusResponse_InitializerStatus_name = map[int32]string{
		0: "CHECKING",
		1: "RESTORING",
		2: "DONE",
		3: "UPGRADING",
	}
	StatusResponse_InitializerStatus_value = map[string]int32{
		"CHECKING":  0,
		"RESTORING": 1,
		"DONE":      2,
		"UPGRADING": 3,
	}
)

func (x StatusResponse_InitializerStatus) Enum() *StatusResponse_InitializerStatus {
	p := new(StatusResponse_InitializerStatus)
	*p = x
	return p
}

func (x StatusResponse_InitializerStatus) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (StatusResponse_InitializerStatus) Descriptor() protoreflect.EnumDescriptor {
	return file_v1_initializer_proto_enumTypes[0].Descriptor()
}

func (StatusResponse_InitializerStatus) Type() protoreflect.EnumType {
	return &file_v1_initializer_proto_enumTypes[0]
}

func (x StatusResponse_InitializerStatus) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use StatusResponse_InitializerStatus.Descriptor instead.
func (StatusResponse_InitializerStatus) EnumDescriptor() ([]byte, []int) {
	return file_v1_initializer_proto_rawDescGZIP(), []int{1, 0}
}

type StatusRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields
}

func (x *StatusRequest) Reset() {
	*x = StatusRequest{}
	mi := &file_v1_initializer_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StatusRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StatusRequest) ProtoMessage() {}

func (x *StatusRequest) ProtoReflect() protoreflect.Message {
	mi := &file_v1_initializer_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StatusRequest.ProtoReflect.Descriptor instead.
func (*StatusRequest) Descriptor() ([]byte, []int) {
	return file_v1_initializer_proto_rawDescGZIP(), []int{0}
}

type StatusResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Status  StatusResponse_InitializerStatus `protobuf:"varint,1,opt,name=status,proto3,enum=v1.StatusResponse_InitializerStatus" json:"status,omitempty"`
	Message string                           `protobuf:"bytes,2,opt,name=message,proto3" json:"message,omitempty"`
}

func (x *StatusResponse) Reset() {
	*x = StatusResponse{}
	mi := &file_v1_initializer_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *StatusResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*StatusResponse) ProtoMessage() {}

func (x *StatusResponse) ProtoReflect() protoreflect.Message {
	mi := &file_v1_initializer_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use StatusResponse.ProtoReflect.Descriptor instead.
func (*StatusResponse) Descriptor() ([]byte, []int) {
	return file_v1_initializer_proto_rawDescGZIP(), []int{1}
}

func (x *StatusResponse) GetStatus() StatusResponse_InitializerStatus {
	if x != nil {
		return x.Status
	}
	return StatusResponse_CHECKING
}

func (x *StatusResponse) GetMessage() string {
	if x != nil {
		return x.Message
	}
	return ""
}

var File_v1_initializer_proto protoreflect.FileDescriptor

var file_v1_initializer_proto_rawDesc = []byte{
	0x0a, 0x14, 0x76, 0x31, 0x2f, 0x69, 0x6e, 0x69, 0x74, 0x69, 0x61, 0x6c, 0x69, 0x7a, 0x65, 0x72,
	0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x02, 0x76, 0x31, 0x22, 0x0f, 0x0a, 0x0d, 0x53, 0x74,
	0x61, 0x74, 0x75, 0x73, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x22, 0xb3, 0x01, 0x0a, 0x0e,
	0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x3c,
	0x0a, 0x06, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x24,
	0x2e, 0x76, 0x31, 0x2e, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e,
	0x73, 0x65, 0x2e, 0x49, 0x6e, 0x69, 0x74, 0x69, 0x61, 0x6c, 0x69, 0x7a, 0x65, 0x72, 0x53, 0x74,
	0x61, 0x74, 0x75, 0x73, 0x52, 0x06, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x12, 0x18, 0x0a, 0x07,
	0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x6d,
	0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x22, 0x49, 0x0a, 0x11, 0x49, 0x6e, 0x69, 0x74, 0x69, 0x61,
	0x6c, 0x69, 0x7a, 0x65, 0x72, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x12, 0x0c, 0x0a, 0x08, 0x43,
	0x48, 0x45, 0x43, 0x4b, 0x49, 0x4e, 0x47, 0x10, 0x00, 0x12, 0x0d, 0x0a, 0x09, 0x52, 0x45, 0x53,
	0x54, 0x4f, 0x52, 0x49, 0x4e, 0x47, 0x10, 0x01, 0x12, 0x08, 0x0a, 0x04, 0x44, 0x4f, 0x4e, 0x45,
	0x10, 0x02, 0x12, 0x0d, 0x0a, 0x09, 0x55, 0x50, 0x47, 0x52, 0x41, 0x44, 0x49, 0x4e, 0x47, 0x10,
	0x03, 0x32, 0x45, 0x0a, 0x12, 0x49, 0x6e, 0x69, 0x74, 0x69, 0x61, 0x6c, 0x69, 0x7a, 0x65, 0x72,
	0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x12, 0x2f, 0x0a, 0x06, 0x53, 0x74, 0x61, 0x74, 0x75,
	0x73, 0x12, 0x11, 0x2e, 0x76, 0x31, 0x2e, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73, 0x52, 0x65, 0x71,
	0x75, 0x65, 0x73, 0x74, 0x1a, 0x12, 0x2e, 0x76, 0x31, 0x2e, 0x53, 0x74, 0x61, 0x74, 0x75, 0x73,
	0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x42, 0x6c, 0x0a, 0x06, 0x63, 0x6f, 0x6d, 0x2e,
	0x76, 0x31, 0x42, 0x10, 0x49, 0x6e, 0x69, 0x74, 0x69, 0x61, 0x6c, 0x69, 0x7a, 0x65, 0x72, 0x50,
	0x72, 0x6f, 0x74, 0x6f, 0x50, 0x01, 0x5a, 0x28, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63,
	0x6f, 0x6d, 0x2f, 0x6d, 0x65, 0x74, 0x61, 0x6c, 0x2d, 0x73, 0x74, 0x61, 0x63, 0x6b, 0x2f, 0x64,
	0x72, 0x6f, 0x70, 0x74, 0x61, 0x69, 0x6c, 0x65, 0x72, 0x2f, 0x61, 0x70, 0x69, 0x2f, 0x76, 0x31,
	0xa2, 0x02, 0x03, 0x56, 0x58, 0x58, 0xaa, 0x02, 0x02, 0x56, 0x31, 0xca, 0x02, 0x02, 0x56, 0x31,
	0xe2, 0x02, 0x0e, 0x56, 0x31, 0x5c, 0x47, 0x50, 0x42, 0x4d, 0x65, 0x74, 0x61, 0x64, 0x61, 0x74,
	0x61, 0xea, 0x02, 0x02, 0x56, 0x31, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_v1_initializer_proto_rawDescOnce sync.Once
	file_v1_initializer_proto_rawDescData = file_v1_initializer_proto_rawDesc
)

func file_v1_initializer_proto_rawDescGZIP() []byte {
	file_v1_initializer_proto_rawDescOnce.Do(func() {
		file_v1_initializer_proto_rawDescData = protoimpl.X.CompressGZIP(file_v1_initializer_proto_rawDescData)
	})
	return file_v1_initializer_proto_rawDescData
}

var file_v1_initializer_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_v1_initializer_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_v1_initializer_proto_goTypes = []any{
	(StatusResponse_InitializerStatus)(0), // 0: v1.StatusResponse.InitializerStatus
	(*StatusRequest)(nil),                 // 1: v1.StatusRequest
	(*StatusResponse)(nil),                // 2: v1.StatusResponse
}
var file_v1_initializer_proto_depIdxs = []int32{
	0, // 0: v1.StatusResponse.status:type_name -> v1.StatusResponse.InitializerStatus
	1, // 1: v1.InitializerService.Status:input_type -> v1.StatusRequest
	2, // 2: v1.InitializerService.Status:output_type -> v1.StatusResponse
	2, // [2:3] is the sub-list for method output_type
	1, // [1:2] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_v1_initializer_proto_init() }
func file_v1_initializer_proto_init() {
	if File_v1_initializer_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_v1_initializer_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_v1_initializer_proto_goTypes,
		DependencyIndexes: file_v1_initializer_proto_depIdxs,
		EnumInfos:         file_v1_initializer_proto_enumTypes,
		MessageInfos:      file_v1_initializer_proto_msgTypes,
	}.Build()
	File_v1_initializer_proto = out.File
	file_v1_initializer_proto_rawDesc = nil
	file_v1_initializer_proto_goTypes = nil
	file_v1_initializer_proto_depIdxs = nil
}
