package postgresrepo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"math/bits"
)

// shortCodePermutationRounds is the number of Feistel rounds. A Feistel network over a domain this small needs more
// rounds than the textbook four to stay indistinguishable from a random permutation when an attacker sees many
// outputs for inputs they can roughly order (their own consecutive links); see ADR-0019.
const shortCodePermutationRounds = 10

// keyedPermutation is a keyed bijection on [0, domain). It is a balanced Feistel network over the smallest even number
// of bits that covers the domain, with HMAC-SHA256 as the round function, and cycle walking to stay inside the domain:
// a value that lands outside it is permuted again until it lands inside. Because the Feistel network is a bijection on
// its bit range, cycle walking makes the restriction a bijection on [0, domain).
//
// Without the key, consecutive inputs give outputs that reveal neither their order nor how many inputs came before.
type keyedPermutation struct {
	key      []byte
	domain   uint64
	halfBits uint
	halfMask uint64
}

func newKeyedPermutation(key []byte, domain uint64) keyedPermutation {
	totalBits := uint(bits.Len64(domain - 1))
	halfBits := (totalBits + 1) / 2
	return keyedPermutation{
		key:      key,
		domain:   domain,
		halfBits: halfBits,
		halfMask: 1<<halfBits - 1,
	}
}

// apply permutes x, which must be in [0, domain).
func (p keyedPermutation) apply(x uint64) uint64 {
	for {
		x = p.feistel(x)
		if x < p.domain {
			return x
		}
	}
}

func (p keyedPermutation) feistel(x uint64) uint64 {
	left, right := x>>p.halfBits, x&p.halfMask
	mac := hmac.New(sha256.New, p.key)
	var input [9]byte
	for round := range shortCodePermutationRounds {
		input[0] = byte(round)
		binary.BigEndian.PutUint64(input[1:], right)
		mac.Reset()
		mac.Write(input[:])
		roundOutput := binary.BigEndian.Uint64(mac.Sum(nil)) & p.halfMask
		left, right = right, left^roundOutput
	}
	return left<<p.halfBits | right
}
