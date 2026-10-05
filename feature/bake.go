package feature

import (
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// BakeU64 writes a little-endian u64 into a clone of template data at offset.
func BakeU64(template solana.Instruction, offset uint16, v uint64) (solana.Instruction, error) {
	if template == nil {
		return nil, fmt.Errorf("bake: nil template")
	}
	data, err := template.Data()
	if err != nil {
		return nil, err
	}
	off := int(offset)
	if off < 0 || off+8 > len(data) {
		return nil, fmt.Errorf("bake: offset %d out of range (len %d)", offset, len(data))
	}
	out := append([]byte(nil), data...)
	binary.LittleEndian.PutUint64(out[off:off+8], v)
	return solana.NewInstruction(template.ProgramID(), template.Accounts(), out), nil
}
