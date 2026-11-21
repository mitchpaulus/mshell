package main

import "encoding/binary"

// fmInput passes an fm_input from the frontend to the file manager. Input
// while none runs is dropped when the next one starts.
func (s *server) fmInput(p []byte) {
	if fileManagerGUI == nil || len(p) < 1 {
		return
	}
	le := binary.LittleEndian
	in := fmGUIInput{kind: p[0]}
	switch {
	case in.kind == fmInputKey && len(p) >= 9:
		in.code, in.escOf = KeyCode(le.Uint32(p[1:])), KeyCode(le.Uint32(p[5:]))
	case in.kind == fmInputPaste:
		in.text = string(p[1:])
	case in.kind == fmInputResize && len(p) >= 5:
		in.cols, in.rows = int(le.Uint16(p[1:])), int(le.Uint16(p[3:]))
	case in.kind == fmInputClose:
	case in.kind == fmInputMouse && len(p) >= 10 && uint64(le.Uint32(p[6:])) <= uint64(len(p)-10):
		in.mouse, in.n = p[1], int(int32(le.Uint32(p[2:])))
		name := p[10:]
		n := le.Uint32(p[6:])
		in.name, in.child = string(name[:n]), string(name[n:])
	default:
		s.protocolError("malformed fm_input")
	}
	select {
	case fileManagerGUI.input <- in:
	default:
		// The file manager isn't reading: drop it rather than stall the
		// protocol.
	}
}
