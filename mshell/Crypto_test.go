package main

import "testing"

// TestCipherNewCoversCipher checks that every member of the built-in
// Cipher enum has an AEAD and a key size, and that no others do.
func TestCipherNewCoversCipher(t *testing.T) {
	d := builtinEnums(t)["Cipher"]
	if d == nil {
		t.Fatal("Cipher is not a built-in enum")
	}
	var news, sizes []string
	for k := range cipherNew {
		news = append(news, k)
	}
	for k := range cipherKeySizes {
		sizes = append(sizes, k)
	}
	sameNames(t, "cipherNew", d.Members, news)
	sameNames(t, "cipherKeySizes", d.Members, sizes)
}

// TestEncryptRoundTrip checks that each cipher decrypts what it encrypts,
// refuses a changed tag, and rejects parts of the wrong length.
func TestEncryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("attack at dawn")
	for member := range cipherNew {
		aead, err := aeadFor(member, key)
		if err != nil {
			t.Fatalf("%s: %v", member, err)
		}
		e, err := encryptRandomNonce(aead, plaintext)
		if err != nil {
			t.Fatalf("%s: %v", member, err)
		}
		if len(e.nonce) != aead.NonceSize() || len(e.ciphertext) != len(plaintext) || len(e.tag) != 16 {
			t.Errorf("%s: lengths %d %d %d", member, len(e.nonce), len(e.ciphertext), len(e.tag))
		}
		got, ok, err := decryptParts(member, aead, e)
		if err != nil || !ok || string(got) != string(plaintext) {
			t.Errorf("%s: decrypt gave %q, %v, %v", member, got, ok, err)
		}
		changed := e
		changed.tag = append(e.tag[:15:15], e.tag[15]^1)
		if _, ok, err := decryptParts(member, aead, changed); ok || err != nil {
			t.Errorf("%s: a changed tag gave %v, %v", member, ok, err)
		}
		short := e
		short.nonce = e.nonce[:4]
		if _, _, err := decryptParts(member, aead, short); err == nil {
			t.Errorf("%s: a short nonce was not an error", member)
		}
		short = e
		short.tag = e.tag[:4]
		if _, _, err := decryptParts(member, aead, short); err == nil {
			t.Errorf("%s: a short tag was not an error", member)
		}
	}
}

// TestConcatBinaryGrowsOnlyNewest checks that '+' writes into a buffer's
// spare space only for the newest binary in it, so no binary changes.
func TestConcatBinaryGrowsOnlyNewest(t *testing.T) {
	a := concatBinary(newBinary([]byte{1}), newBinary([]byte{2}))
	b := concatBinary(a, newBinary([]byte{3}))
	if cap(a.Data) <= len(a.Data) || &b.Data[0] != &a.Data[0] {
		t.Fatal("joining onto the newest binary copied it")
	}
	c := concatBinary(a, newBinary([]byte{4}))
	if &c.Data[0] == &a.Data[0] {
		t.Fatal("joining onto an older binary did not copy it")
	}
	if string(b.Data) != "\x01\x02\x03" || string(c.Data) != "\x01\x02\x04" || string(a.Data) != "\x01\x02" {
		t.Errorf("got a=%x b=%x c=%x", a.Data, b.Data, c.Data)
	}
	// A slice shares the bytes but not the record, so it copies.
	s := newBinary(b.Data[:1])
	d := concatBinary(s, newBinary([]byte{5}))
	if &d.Data[0] == &b.Data[0] || string(b.Data) != "\x01\x02\x03" {
		t.Error("joining onto a slice wrote into its buffer")
	}
}

// TestAeadForCacheMatchesKeyAndCipher checks that aeadFor reuses an AEAD
// only for the same cipher and key bytes, even when the caller's key
// changes after the AEAD was made.
func TestAeadForCacheMatchesKeyAndCipher(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("attack at dawn")
	aead, err := aeadFor("cipherAesGcm", key)
	if err != nil {
		t.Fatal(err)
	}
	e, err := encryptRandomNonce(aead, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := aeadFor("cipherAesGcm", make([]byte, 32)); again != aead {
		t.Error("the same cipher and key did not reuse the AEAD")
	}
	key[0] = 1
	other, err := aeadFor("cipherAesGcm", key)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := decryptParts("cipherAesGcm", other, e); ok {
		t.Error("a changed key decrypted with the old key's AEAD")
	}
	if c, _ := aeadFor("cipherChaCha20Poly1305", key); c == other {
		t.Error("a different cipher reused the AEAD")
	}
	if _, err := aeadFor("cipherAesGcm", key[:5]); err == nil {
		t.Error("a 5 byte key was not an error")
	}
}

// TestBitwiseBinaryMatchesBytes checks and, or and xor against each byte
// for lengths that do and do not fill whole words.
func TestBitwiseBinaryMatchesBytes(t *testing.T) {
	for n := 0; n < 70; n++ {
		a, b := make([]byte, n), make([]byte, n)
		for i := range a {
			a[i], b[i] = byte(i*37+11), byte(i*101+3)
		}
		for _, op := range []string{"and", "or", "xor"} {
			got, err := bitwiseBinary(op, a, b)
			if err != nil {
				t.Fatal(err)
			}
			for i := range a {
				if want := byte(bitwiseInt(op, int(a[i]), int(b[i]))); got[i] != want {
					t.Fatalf("%s length %d byte %d: got %x, want %x", op, n, i, got[i], want)
				}
			}
		}
	}
}
