// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: cosmos/crypto/hd/hd.proto

package hd

import (
	fmt "fmt"
	_ "github.com/gogo/protobuf/gogoproto"
	proto "github.com/gogo/protobuf/proto"
	io "io"
	math "math"
	math_bits "math/bits"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion3 // please upgrade the proto package

type BIP44Params struct {
	Purpose      uint32 `protobuf:"varint,1,opt,name=purpose,proto3" json:"purpose,omitempty"`
	CoinType     uint32 `protobuf:"varint,2,opt,name=coinType,proto3" json:"coinType,omitempty"`
	Account      uint32 `protobuf:"varint,3,opt,name=account,proto3" json:"account,omitempty"`
	Change       bool   `protobuf:"varint,4,opt,name=change,proto3" json:"change,omitempty"`
	AddressIndex uint32 `protobuf:"varint,5,opt,name=addressIndex,proto3" json:"addressIndex,omitempty"`
}

func (m *BIP44Params) Reset()         { *m = BIP44Params{} }
func (m *BIP44Params) String() string { return proto.CompactTextString(m) }
func (*BIP44Params) ProtoMessage()    {}
func (*BIP44Params) Descriptor() ([]byte, []int) {
	return fileDescriptor_388362624e554f43, []int{0}
}
func (m *BIP44Params) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *BIP44Params) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_BIP44Params.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *BIP44Params) XXX_Merge(src proto.Message) {
	xxx_messageInfo_BIP44Params.Merge(m, src)
}
func (m *BIP44Params) XXX_Size() int {
	return m.Size()
}
func (m *BIP44Params) XXX_DiscardUnknown() {
	xxx_messageInfo_BIP44Params.DiscardUnknown(m)
}

var xxx_messageInfo_BIP44Params proto.InternalMessageInfo

func init() {
	proto.RegisterType((*BIP44Params)(nil), "cosmos.crypto.hd.BIP44Params")
}

func init() { proto.RegisterFile("cosmos/crypto/hd/hd.proto", fileDescriptor_388362624e554f43) }

var fileDescriptor_388362624e554f43 = []byte{
	// 243 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x92, 0x4c, 0xce, 0x2f, 0xce,
	0xcd, 0x2f, 0xd6, 0x4f, 0x2e, 0xaa, 0x2c, 0x28, 0xc9, 0xd7, 0xcf, 0x48, 0xd1, 0xcf, 0x48, 0xd1,
	0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0x12, 0x80, 0x48, 0xe9, 0x41, 0xa4, 0xf4, 0x32, 0x52, 0xa4,
	0x44, 0xd2, 0xf3, 0xd3, 0xf3, 0xc1, 0x92, 0xfa, 0x20, 0x16, 0x44, 0x9d, 0xd2, 0x4c, 0x46, 0x2e,
	0x6e, 0x27, 0xcf, 0x00, 0x13, 0x93, 0x80, 0xc4, 0xa2, 0xc4, 0xdc, 0x62, 0x21, 0x09, 0x2e, 0xf6,
	0x82, 0xd2, 0xa2, 0x82, 0xfc, 0xe2, 0x54, 0x09, 0x46, 0x05, 0x46, 0x0d, 0xde, 0x20, 0x18, 0x57,
	0x48, 0x8a, 0x8b, 0x23, 0x39, 0x3f, 0x33, 0x2f, 0xa4, 0xb2, 0x20, 0x55, 0x82, 0x09, 0x2c, 0x05,
	0xe7, 0x83, 0x74, 0x25, 0x26, 0x27, 0xe7, 0x97, 0xe6, 0x95, 0x48, 0x30, 0x43, 0x74, 0x41, 0xb9,
	0x42, 0x62, 0x5c, 0x6c, 0xc9, 0x19, 0x89, 0x79, 0xe9, 0xa9, 0x12, 0x2c, 0x0a, 0x8c, 0x1a, 0x1c,
	0x41, 0x50, 0x9e, 0x90, 0x12, 0x17, 0x4f, 0x62, 0x4a, 0x4a, 0x51, 0x6a, 0x71, 0xb1, 0x67, 0x5e,
	0x4a, 0x6a, 0x85, 0x04, 0x2b, 0x58, 0x1b, 0x8a, 0x98, 0x93, 0xcb, 0x89, 0x87, 0x72, 0x0c, 0x27,
	0x1e, 0xc9, 0x31, 0x5e, 0x78, 0x24, 0xc7, 0xf8, 0xe0, 0x91, 0x1c, 0xe3, 0x84, 0xc7, 0x72, 0x0c,
	0x17, 0x1e, 0xcb, 0x31, 0xdc, 0x78, 0x2c, 0xc7, 0x10, 0xa5, 0x96, 0x9e, 0x59, 0x92, 0x51, 0x9a,
	0xa4, 0x97, 0x9c, 0x9f, 0xab, 0x0f, 0x0b, 0x07, 0x30, 0xa5, 0x5b, 0x9c, 0x92, 0x8d, 0x08, 0x92,
	0x24, 0x36, 0xb0, 0x47, 0x8d, 0x01, 0x01, 0x00, 0x00, 0xff, 0xff, 0x5a, 0x37, 0xd3, 0x24, 0x2d,
	0x01, 0x00, 0x00,
}

func (m *BIP44Params) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *BIP44Params) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *BIP44Params) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.AddressIndex != 0 {
		i = encodeVarintHd(dAtA, i, uint64(m.AddressIndex))
		i--
		dAtA[i] = 0x28
	}
	if m.Change {
		i--
		if m.Change {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x20
	}
	if m.Account != 0 {
		i = encodeVarintHd(dAtA, i, uint64(m.Account))
		i--
		dAtA[i] = 0x18
	}
	if m.CoinType != 0 {
		i = encodeVarintHd(dAtA, i, uint64(m.CoinType))
		i--
		dAtA[i] = 0x10
	}
	if m.Purpose != 0 {
		i = encodeVarintHd(dAtA, i, uint64(m.Purpose))
		i--
		dAtA[i] = 0x8
	}
	return len(dAtA) - i, nil
}

func encodeVarintHd(dAtA []byte, offset int, v uint64) int {
	offset -= sovHd(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}
func (m *BIP44Params) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if m.Purpose != 0 {
		n += 1 + sovHd(uint64(m.Purpose))
	}
	if m.CoinType != 0 {
		n += 1 + sovHd(uint64(m.CoinType))
	}
	if m.Account != 0 {
		n += 1 + sovHd(uint64(m.Account))
	}
	if m.Change {
		n += 2
	}
	if m.AddressIndex != 0 {
		n += 1 + sovHd(uint64(m.AddressIndex))
	}
	return n
}

func sovHd(x uint64) (n int) {
	return (math_bits.Len64(x|1) + 6) / 7
}
func sozHd(x uint64) (n int) {
	return sovHd(uint64((x << 1) ^ uint64((int64(x) >> 63))))
}
func (m *BIP44Params) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowHd
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: BIP44Params: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: BIP44Params: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Purpose", wireType)
			}
			m.Purpose = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowHd
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.Purpose |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 2:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field CoinType", wireType)
			}
			m.CoinType = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowHd
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.CoinType |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 3:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Account", wireType)
			}
			m.Account = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowHd
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.Account |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 4:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Change", wireType)
			}
			var v int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowHd
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				v |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			m.Change = bool(v != 0)
		case 5:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field AddressIndex", wireType)
			}
			m.AddressIndex = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowHd
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.AddressIndex |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipHd(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthHd
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func skipHd(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	depth := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, ErrIntOverflowHd
			}
			if iNdEx >= l {
				return 0, io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		wireType := int(wire & 0x7)
		switch wireType {
		case 0:
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowHd
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				iNdEx++
				if dAtA[iNdEx-1] < 0x80 {
					break
				}
			}
		case 1:
			iNdEx += 8
		case 2:
			var length int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowHd
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				length |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if length < 0 {
				return 0, ErrInvalidLengthHd
			}
			iNdEx += length
		case 3:
			depth++
		case 4:
			if depth == 0 {
				return 0, ErrUnexpectedEndOfGroupHd
			}
			depth--
		case 5:
			iNdEx += 4
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
		if iNdEx < 0 {
			return 0, ErrInvalidLengthHd
		}
		if depth == 0 {
			return iNdEx, nil
		}
	}
	return 0, io.ErrUnexpectedEOF
}

var (
	ErrInvalidLengthHd        = fmt.Errorf("proto: negative length found during unmarshaling")
	ErrIntOverflowHd          = fmt.Errorf("proto: integer overflow")
	ErrUnexpectedEndOfGroupHd = fmt.Errorf("proto: unexpected end of group")
)
