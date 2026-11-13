package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"sync/atomic"

	"golang.org/x/crypto/chacha20poly1305"
)

// Byte and encryption builtins: the bitwise words on int and binary,
// cryptoRandom, and encrypt/decrypt with a Cipher.

// bitwiseInt applies and, or or xor to two ints.
func bitwiseInt(op string, a, b int) int {
	switch op {
	case "and":
		return a & b
	case "or":
		return a | b
	default:
		return a ^ b
	}
}

// bitwiseBinary applies and, or or xor to each byte of a and b, which must
// have the same length.
func bitwiseBinary(op string, a, b []byte) ([]byte, error) {
	if len(a) != len(b) {
		return nil, checkedErrorf("'%s' needs two binaries of the same length, got lengths %d and %d.\n", op, len(a), len(b))
	}
	out := make([]byte, len(a))
	// The switch is outside the loops so op is compared once, not per byte.
	switch op {
	case "and":
		for i := range a {
			out[i] = a[i] & b[i]
		}
	case "or":
		for i := range a {
			out[i] = a[i] | b[i]
		}
	default:
		subtle.XORBytes(out, a, b)
	}
	return out, nil
}

// cipherNew makes the AEAD for each member of the built-in Cipher enum
// from a key. TestCipherNewCoversCipher keeps it in step with
// builtinDeclSource.
var cipherNew = map[string]func(key []byte) (cipher.AEAD, error){
	"cipherAesGcm":            newAesGcm,
	"cipherChaCha20Poly1305":  chacha20poly1305.New,
	"cipherXChaCha20Poly1305": chacha20poly1305.NewX,
}

// cipherKeySizes names the key lengths each Cipher member takes, for errors.
var cipherKeySizes = map[string]string{
	"cipherAesGcm":            "16, 24 or 32",
	"cipherChaCha20Poly1305":  "32",
	"cipherXChaCha20Poly1305": "32",
}

func newAesGcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// aeadCacheEntry is the last AEAD aeadFor made, with the cipher and a
// copy of the key it was made from.
type aeadCacheEntry struct {
	member string
	key    []byte
	aead   cipher.AEAD
}

// lastAead lets a loop that encrypts or decrypts many items with one key
// set the key up once. Pipeline stages run at the same time, so it is
// atomic; two stages using it at once at worst make the AEAD again.
var lastAead atomic.Pointer[aeadCacheEntry]

// aeadFor gives the AEAD that member names, using key.
func aeadFor(member string, key []byte) (cipher.AEAD, error) {
	if e := lastAead.Load(); e != nil && e.member == member && bytes.Equal(e.key, key) {
		return e.aead, nil
	}
	aead, err := newAeadFor(member, key)
	if err != nil {
		return nil, err
	}
	lastAead.Store(&aeadCacheEntry{member, bytes.Clone(key), aead})
	return aead, nil
}

// newAeadFor makes the AEAD that member names, using key.
func newAeadFor(member string, key []byte) (cipher.AEAD, error) {
	newAead, ok := cipherNew[member]
	if !ok {
		return nil, fmt.Errorf("unknown cipher %s", member)
	}
	aead, err := newAead(key)
	if err != nil {
		return nil, checkedErrorf("%s takes a key of %s bytes, got %d.\n", member, cipherKeySizes[member], len(key))
	}
	return aead, nil
}

// encrypted is what encrypt gives and decrypt takes: the Encrypted type.
type encrypted struct {
	nonce, ciphertext, tag []byte
}

// encryptRandomNonce encrypts plaintext with a new random nonce.
func encryptRandomNonce(aead cipher.AEAD, plaintext []byte) (encrypted, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return encrypted{}, err
	}
	sealed := aead.Seal(nil, nonce, plaintext, nil)
	split := len(sealed) - aead.Overhead()
	return encrypted{nonce, sealed[:split], sealed[split:]}, nil
}

// decryptParts reverses encryptRandomNonce. A nonce or tag of the wrong
// length is an error; a wrong key or changed data gives false.
func decryptParts(member string, aead cipher.AEAD, e encrypted) ([]byte, bool, error) {
	if len(e.nonce) != aead.NonceSize() {
		return nil, false, checkedErrorf("%s takes a nonce of %d bytes, got %d.\n", member, aead.NonceSize(), len(e.nonce))
	}
	if len(e.tag) != aead.Overhead() {
		return nil, false, checkedErrorf("%s takes a tag of %d bytes, got %d.\n", member, aead.Overhead(), len(e.tag))
	}
	// Open decrypts into the buffer it reads, so the plaintext needs no
	// second buffer.
	buf := make([]byte, 0, len(e.ciphertext)+len(e.tag))
	buf = append(append(buf, e.ciphertext...), e.tag...)
	plaintext, err := aead.Open(buf[:0], e.nonce, buf, nil)
	if err != nil {
		return nil, false, nil
	}
	return plaintext, true, nil
}

// popBinary pops a binary for word, naming it what in errors.
func (state *EvalState) popBinary(t *Token, stack *MShellStack, word string, what string) ([]byte, *EvalResult) {
	obj, err := stack.Pop()
	if err != nil {
		return nil, state.mismatchPtr(fmt.Sprintf("%d:%d: '%s' needs a binary %s, but the stack is empty.\n", t.Line, t.Column, word, what))
	}
	b, ok := obj.(MShellBinary)
	if !ok {
		return nil, state.mismatchPtr(fmt.Sprintf("%d:%d: '%s' expected a binary %s, found a %s (%s).\n", t.Line, t.Column, word, what, obj.TypeName(), obj.DebugString()))
	}
	return b.Data, nil
}

// popCipher pops a Cipher member's name for word.
func (state *EvalState) popCipher(t *Token, stack *MShellStack, word string) (string, *EvalResult) {
	obj, err := stack.Pop()
	if err != nil {
		return "", state.mismatchPtr(fmt.Sprintf("%d:%d: '%s' needs a Cipher, but the stack is empty.\n", t.Line, t.Column, word))
	}
	e, ok := obj.(*MShellEnum)
	if !ok || e.EnumName != "Cipher" {
		return "", state.mismatchPtr(fmt.Sprintf("%d:%d: '%s' expected a Cipher on top of the stack, found a %s (%s).\n", t.Line, t.Column, word, obj.TypeName(), obj.DebugString()))
	}
	return e.Member, nil
}

// evalBitwise runs and, or or xor on two ints or two binaries, and xor on
// two bools. and/or on bools are handled by their own case.
func (state *EvalState) evalBitwise(t *Token, stack *MShellStack, obj1 MShellObject, obj2 MShellObject) *EvalResult {
	switch a := obj2.(type) {
	case MShellInt:
		if b, ok := obj1.(MShellInt); ok {
			stack.Push(MShellInt{bitwiseInt(t.Lexeme, a.Value, b.Value)})
			return nil
		}
	case MShellBinary:
		if b, ok := obj1.(MShellBinary); ok {
			out, err := bitwiseBinary(t.Lexeme, a.Data, b.Data)
			if err != nil {
				return state.failErrPtr(err, fmt.Sprintf("%d:%d: %s", t.Line, t.Column, err.Error()))
			}
			stack.Push(newBinary(out))
			return nil
		}
	case MShellBool:
		if b, ok := obj1.(MShellBool); ok && t.Lexeme == "xor" {
			stack.Push(MShellBool{a.Value != b.Value})
			return nil
		}
	}
	return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
}

// evalToBinary runs toBinary: a list of ints, each 0 to 255, to binary.
func (state *EvalState) evalToBinary(t *Token, stack *MShellStack) *EvalResult {
	obj, err := stack.Pop()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do 'toBinary' operation on an empty stack.\n", t.Line, t.Column))
	}
	list, ok := obj.(*MShellList)
	if !ok {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'toBinary' expected a list of int, found a %s (%s).\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
	}
	out := make([]byte, len(list.Items))
	for i, item := range list.Items {
		n, ok := item.(MShellInt)
		if !ok {
			return state.mismatchPtr(fmt.Sprintf("%d:%d: 'toBinary' expected a list of int, item %d is a %s (%s).\n", t.Line, t.Column, i, item.TypeName(), item.DebugString()))
		}
		if n.Value < 0 || n.Value > 255 {
			return state.checkedPtr(fmt.Sprintf("%d:%d: 'toBinary' takes ints from 0 to 255, item %d is %d.\n", t.Line, t.Column, i, n.Value))
		}
		out[i] = byte(n.Value)
	}
	stack.Push(newBinary(out))
	return nil
}

// evalByteList runs byteList: each byte of a binary as an int.
func (state *EvalState) evalByteList(t *Token, stack *MShellStack) *EvalResult {
	b, r := state.popBinary(t, stack, "byteList", "value")
	if r != nil {
		return r
	}
	list := NewList(len(b))
	for i, c := range b {
		list.Items[i] = MShellInt{int(c)}
	}
	stack.Push(list)
	return nil
}

// evalCryptoRandom runs cryptoRandom: n bytes from crypto/rand.
func (state *EvalState) evalCryptoRandom(t *Token, stack *MShellStack) *EvalResult {
	obj, err := stack.Pop()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do 'cryptoRandom' operation on an empty stack.\n", t.Line, t.Column))
	}
	n, ok := obj.(MShellInt)
	if !ok {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'cryptoRandom' expected an int, found a %s (%s).\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
	}
	if n.Value < 0 {
		return state.checkedPtr(fmt.Sprintf("%d:%d: 'cryptoRandom' needs a count of 0 or more, got %d.\n", t.Line, t.Column, n.Value))
	}
	out := make([]byte, n.Value)
	if _, err := rand.Read(out); err != nil {
		return state.checkedPtr(fmt.Sprintf("%d:%d: 'cryptoRandom' could not read random bytes: %s\n", t.Line, t.Column, err.Error()))
	}
	stack.Push(newBinary(out))
	return nil
}

// evalEncrypt runs encrypt: (binary:data binary:key Cipher -- Encrypted).
func (state *EvalState) evalEncrypt(t *Token, stack *MShellStack) *EvalResult {
	member, r := state.popCipher(t, stack, "encrypt")
	if r != nil {
		return r
	}
	key, r := state.popBinary(t, stack, "encrypt", "key")
	if r != nil {
		return r
	}
	data, r := state.popBinary(t, stack, "encrypt", "input")
	if r != nil {
		return r
	}
	aead, err := aeadFor(member, key)
	if err != nil {
		return state.failErrPtr(err, fmt.Sprintf("%d:%d: 'encrypt': %s", t.Line, t.Column, err.Error()))
	}
	e, err := encryptRandomNonce(aead, data)
	if err != nil {
		return state.checkedPtr(fmt.Sprintf("%d:%d: 'encrypt' could not read a random nonce: %s\n", t.Line, t.Column, err.Error()))
	}
	dict := NewDict()
	dict.Items["nonce"] = newBinary(e.nonce)
	dict.Items["ciphertext"] = newBinary(e.ciphertext)
	dict.Items["tag"] = newBinary(e.tag)
	stack.Push(dict)
	return nil
}

// evalDecrypt runs decrypt:
// (Encrypted binary:key Cipher -- Maybe[binary]).
func (state *EvalState) evalDecrypt(t *Token, stack *MShellStack) *EvalResult {
	member, r := state.popCipher(t, stack, "decrypt")
	if r != nil {
		return r
	}
	key, r := state.popBinary(t, stack, "decrypt", "key")
	if r != nil {
		return r
	}
	obj, err := stack.Pop()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'decrypt' needs an Encrypted dictionary, but the stack is empty.\n", t.Line, t.Column))
	}
	dict, ok := obj.(*MShellDict)
	if !ok {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'decrypt' expected an Encrypted dictionary, found a %s (%s).\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
	}
	var parts [3][]byte
	for i, field := range []string{"nonce", "ciphertext", "tag"} {
		b, ok := dict.Items[field].(MShellBinary)
		if !ok {
			return state.mismatchPtr(fmt.Sprintf("%d:%d: 'decrypt' expected a binary '%s' field in its Encrypted dictionary.\n", t.Line, t.Column, field))
		}
		parts[i] = b.Data
	}
	aead, err := aeadFor(member, key)
	if err != nil {
		return state.failErrPtr(err, fmt.Sprintf("%d:%d: 'decrypt': %s", t.Line, t.Column, err.Error()))
	}
	plaintext, ok, err := decryptParts(member, aead, encrypted{parts[0], parts[1], parts[2]})
	if err != nil {
		return state.failErrPtr(err, fmt.Sprintf("%d:%d: 'decrypt': %s", t.Line, t.Column, err.Error()))
	}
	if ok {
		stack.Push(&Maybe{obj: newBinary(plaintext)})
	} else {
		stack.Push(&Maybe{obj: nil})
	}
	return nil
}
