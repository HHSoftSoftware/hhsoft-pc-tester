package inventory

// TouchscreenCandidate is discovery, not descriptor support. A DIRECT absolute
// input remains visible even when its name suggests an indirect device. Unknown
// shapes are rejected by the functional descriptor and evidence validators.
// The word width is observed from the kernel, never selected by registry text.
func TouchscreenCandidate(category, absolute, properties string, wordBits int) bool {
	return category == "touchscreen" ||
		absolute != "" && inputCapabilityBitWidth(properties, 1, wordBits)
}
