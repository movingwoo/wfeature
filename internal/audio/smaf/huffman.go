package smaf

import "fmt"

// The compressed score track dialect stores its sequence as a Huffman tree
// followed by the coded bytes. The tree is written in preorder: a 1 bit is an
// internal node whose two children follow, a 0 bit is a leaf holding the next
// eight bits as its byte.

const huffmanLeafCount = 256

type bitReader struct {
	data       []byte
	byteOffset int
	bitOffset  uint8
}

func (r *bitReader) bit() (uint8, bool) {
	if r.byteOffset >= len(r.data) {
		return 0, false
	}
	value := r.data[r.byteOffset] >> (7 - r.bitOffset) & 1
	r.bitOffset++
	if r.bitOffset == 8 {
		r.bitOffset = 0
		r.byteOffset++
	}
	return value, true
}

func (r *bitReader) bits(count uint8) (int, bool) {
	value := 0
	for index := uint8(0); index < count; index++ {
		bit, ok := r.bit()
		if !ok {
			return 0, false
		}
		value = value<<1 | int(bit)
	}
	return value, true
}

type huffmanTree struct {
	left  [2*huffmanLeafCount - 1]int
	right [2*huffmanLeafCount - 1]int
	next  int
}

// read builds one subtree and answers its node index. Leaves are their own
// byte value, so a node index below huffmanLeafCount is the decoded byte.
func (tree *huffmanTree) read(r *bitReader) (int, bool) {
	bit, ok := r.bit()
	if !ok {
		return 0, false
	}
	if bit == 0 {
		return r.bits(8)
	}
	node := tree.next
	tree.next++
	if tree.next > 2*huffmanLeafCount-1 {
		return 0, false
	}
	left, ok := tree.read(r)
	if !ok {
		return 0, false
	}
	right, ok := tree.read(r)
	if !ok {
		return 0, false
	}
	tree.left[node], tree.right[node] = left, right
	return node, true
}

func huffmanDecode(decodedLength int, source []byte) ([]byte, error) {
	if decodedLength < 0 {
		return nil, fmt.Errorf("negative Huffman decoded length %d", decodedLength)
	}
	reader := &bitReader{data: source}
	tree := &huffmanTree{next: huffmanLeafCount}
	root, ok := tree.read(reader)
	if !ok {
		return nil, fmt.Errorf("truncated Huffman tree")
	}

	decoded := make([]byte, 0, decodedLength)
	for index := 0; index < decodedLength; index++ {
		node := root
		for node >= huffmanLeafCount {
			bit, ok := reader.bit()
			if !ok {
				return nil, fmt.Errorf("truncated Huffman data after %d bytes", index)
			}
			if bit == 1 {
				node = tree.right[node]
			} else {
				node = tree.left[node]
			}
		}
		decoded = append(decoded, byte(node))
	}
	return decoded, nil
}
