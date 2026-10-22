package qr203

// International character substitution table for ESC R n.
// Maps 12 specific ASCII codes (0x23, 0x24, 0x40, 0x5B, 0x5C, 0x5D, 0x5E, 0x60, 0x7B, 0x7C, 0x7D, 0x7E)
// according to Appendix 2 of the CSN-A2 / QR203 Hardware Manual.

type IntlSubstitutions struct {
	P23 rune // 0x23 #
	P24 rune // 0x24 $
	P40 rune // 0x40 @
	P5B rune // 0x5B [
	P5C rune // 0x5C \
	P5D rune // 0x5D ]
	P5E rune // 0x5E ^
	P60 rune // 0x60 `
	P7B rune // 0x7B {
	P7C rune // 0x7C |
	P7D rune // 0x7D }
	P7E rune // 0x7E ~
}

var intlSets = [16]IntlSubstitutions{
	// 0: U.S.A.
	{'#', '$', '@', '[', '\\', ']', '^', '`', '{', '|', '}', '~'},
	// 1: France
	{'#', '$', 'à', '°', 'ç', '§', '^', '`', 'é', 'ù', 'è', '¨'},
	// 2: Germany
	{'#', '$', '§', 'Ä', 'Ö', 'Ü', '^', '`', 'ä', 'ö', 'ü', 'ß'},
	// 3: U.K.
	{'£', '$', '@', '[', '\\', ']', '^', '`', '{', '|', '}', '~'},
	// 4: Denmark I
	{'#', '$', '@', 'Æ', 'Ø', 'Å', '^', '`', 'æ', 'ø', 'å', '~'},
	// 5: Sweden
	{'#', '¤', 'É', 'Ä', 'Ö', 'Å', 'Ü', 'é', 'ä', 'ö', 'å', 'ü'},
	// 6: Italy
	{'#', '$', '@', '°', '\\', 'é', '^', 'ù', 'à', 'ò', 'è', 'ì'},
	// 7: Spain I
	{'P', '$', '@', '¡', 'Ñ', '¿', '^', '`', '¨', 'ñ', '}', '~'},
	// 8: Japan
	{'#', '$', '@', '[', '¥', ']', '^', '`', '{', '|', '}', '~'},
	// 9: Norway
	{'#', '¤', 'É', 'Æ', 'Ø', 'Å', 'Ü', 'é', 'æ', 'ø', 'å', 'ü'},
	// 10: Denmark II
	{'#', '$', 'É', 'Æ', 'Ø', 'Å', 'Ü', 'é', 'æ', 'ø', 'å', 'ü'},
	// 11: Spain II
	{'#', '$', 'á', '¡', 'Ñ', '¿', 'é', '`', 'í', 'ñ', 'ó', 'ú'},
	// 12: Latin America
	{'#', '$', 'á', '¡', 'Ñ', '¿', 'é', 'ü', 'í', 'ñ', 'ó', 'ú'},
	// 13: Korea
	{'#', '$', '@', '[', '₩', ']', '^', '`', '{', '|', '}', '~'},
	// 14: Slovenia/Croatia
	{'#', '$', 'Ž', 'Š', 'Ď', 'Ć', 'Č', 'ž', 'š', 'ď', 'ć', 'č'},
	// 15: China
	{'#', '¥', '@', '[', '\\', ']', '^', '`', '{', '|', '}', '~'},
}

// ApplyIntlCharset checks if the ASCII byte is one of the 12 international substitution points
// for the given charset index (0..15). If so, it returns the substituted rune.
func ApplyIntlCharset(b byte, intlSet int) rune {
	if intlSet < 0 || intlSet >= len(intlSets) {
		intlSet = 0
	}
	s := intlSets[intlSet]
	switch b {
	case 0x23:
		return s.P23
	case 0x24:
		return s.P24
	case 0x40:
		return s.P40
	case 0x5B:
		return s.P5B
	case 0x5C:
		return s.P5C
	case 0x5D:
		return s.P5D
	case 0x5E:
		return s.P5E
	case 0x60:
		return s.P60
	case 0x7B:
		return s.P7B
	case 0x7C:
		return s.P7C
	case 0x7D:
		return s.P7D
	case 0x7E:
		return s.P7E
	default:
		return rune(b)
	}
}

// CP437 table mapping bytes 0x80..0xFF to unicode runes
var cp437Runes = [128]rune{
	'Ç', 'ü', 'é', 'â', 'ä', 'à', 'å', 'ç', 'ê', 'ë', 'è', 'ï', 'î', 'ì', 'Ä', 'Å',
	'É', 'æ', 'Æ', 'ô', 'ö', 'ò', 'û', 'ù', 'ÿ', 'Ö', 'Ü', '¢', '£', '¥', '₧', 'ƒ',
	'á', 'í', 'ó', 'ú', 'ñ', 'Ñ', 'ª', 'º', '¿', '⌐', '¬', '½', '¼', '¡', '«', '»',
	'░', '▒', '▓', '│', '┤', '╡', '╢', '╖', '╕', '╣', '║', '╗', '╝', '╜', '╛', '┐',
	'└', '┴', '┬', '├', '─', '┼', '╞', '╟', '╚', '╔', '╩', '╦', '╠', '═', '╬', '╧',
	'╨', '╤', '╥', '╙', '╘', '╒', '╓', '╫', '╪', '┘', '┌', '█', '▄', '▌', '▐', '▀',
	'α', 'ß', 'Γ', 'π', 'Σ', 'σ', 'µ', 'τ', 'Φ', 'Θ', 'Ω', 'δ', '∞', 'φ', 'ε', '∩',
	'≡', '±', '≥', '≤', '⌠', '⌡', '÷', '≈', '°', '·', '·', '√', 'ⁿ', '²', '■', ' ',
}

// CP850 table mapping bytes 0x80..0xFF
var cp850Runes = [128]rune{
	'Ç', 'ü', 'é', 'â', 'ä', 'à', 'å', 'ç', 'ê', 'ë', 'è', 'ï', 'î', 'ì', 'Ä', 'Å',
	'É', 'æ', 'Æ', 'ô', 'ö', 'ò', 'û', 'ù', 'ÿ', 'Ö', 'Ü', 'ø', '£', 'Ø', '×', 'ƒ',
	'á', 'í', 'ó', 'ú', 'ñ', 'Ñ', 'ª', 'º', '¿', '®', '¬', '½', '¼', '¡', '«', '»',
	'░', '▒', '▓', '│', '┤', 'Á', 'Â', 'À', '©', '╣', '║', '╗', '╝', '¢', '¥', '┐',
	'└', '┴', '┬', '├', '─', '┼', 'ã', 'Ã', '╚', '╔', '╩', '╦', '╠', '═', '╬', '¤',
	'ð', 'Ð', 'Ê', 'Ë', 'È', 'I', 'Í', 'Î', 'Ï', '┘', '┌', '█', '▄', '¦', 'Ì', '▀',
	'Ó', 'ß', 'Ô', 'Ò', 'õ', 'Õ', 'µ', 'þ', 'Þ', 'Ú', 'Û', 'Ù', 'ý', 'Ý', '¯', '´',
	'­', '±', '‗', '¾', '¶', '§', '÷', '¸', '°', '¨', '·', '¹', '³', '²', '■', ' ',
}

// DecodeByteToRune resolves a byte through the active CodePage and IntlCharset.
func DecodeByteToRune(b byte, codePage int, intlSet int) rune {
	if b < 0x80 {
		return ApplyIntlCharset(b, intlSet)
	}

	idx := int(b - 0x80)
	switch codePage {
	case 2: // CP850 [Multilingual]
		return cp850Runes[idx]
	case 19: // CP858 [Multilingual Latin I + Euro]
		if b == 0xD5 {
			return '€'
		}
		return cp850Runes[idx]
	case 16, 23: // WCP1252 / ISO-8859-1 [West Europe]
		if b == 0x80 {
			return '€'
		}
		if b >= 0xA0 {
			return rune(b)
		}
		return cp437Runes[idx]
	default: // Default CP437
		return cp437Runes[idx]
	}
}
