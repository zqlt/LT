





package blake2b

func f(h *[8]uint64, m *[16]uint64, c0, c1 uint64, flag uint64, rounds uint64) {
	fGeneric(h, m, c0, c1, flag, rounds)
}
