package fec

import "errors"

var errRSLength = errors.New("fec: RS word must contain exactly 204 bytes")
var errRSUncorrectable = errors.New("fec: uncorrectable RS word")

// GF(256) uses p(x)=x^8+x^4+x^3+x^2+1, with primitive element alpha=2.
var gfExp, gfLog = func() ([510]byte, [256]byte) {
	var exp [510]byte
	var log [256]byte
	x := 1
	for i := 0; i < 255; i++ {
		exp[i] = byte(x)
		log[x] = byte(i)
		x <<= 1
		if x&256 != 0 {
			x ^= 0x11d
		}
	}
	for i := 255; i < len(exp); i++ {
		exp[i] = exp[i-255]
	}
	return exp, log
}()

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}
func gfDiv(a, b byte) byte {
	if a == 0 {
		return 0
	}
	return gfExp[(int(gfLog[a])-int(gfLog[b])+255)%255]
}
func syndromes(word []byte) (s [16]byte) {
	for i := range s {
		root := gfExp[i]
		for _, c := range word {
			s[i] = gfMul(s[i], root) ^ c
		}
	}
	return
}
func allZero(s [16]byte) bool {
	for _, v := range s {
		if v != 0 {
			return false
		}
	}
	return true
}

// DecodeRS corrects up to eight corrupted bytes in a shortened RS(255,239)
// word with roots alpha^0 through alpha^15. The omitted 51 leading zero bytes
// leave exactly 204 transmitted bytes (188 data followed by 16 parity bytes).
// On success the entire word is corrected in place and the number of changed
// bytes is returned. On error the input is unchanged. As with any bounded-
// distance RS decoder, some patterns beyond eight errors can miscorrect to
// another valid codeword; no decoder can universally detect nine errors.
func DecodeRS(word []byte) (int, error) {
	if len(word) != 204 {
		return 0, errRSLength
	}
	s := syndromes(word)
	if allZero(s) {
		return 0, nil
	}
	// Berlekamp-Massey computes the low-degree-first locator polynomial.
	var locator, previous [17]byte
	locator[0] = 1
	previous[0] = 1
	degree, shift := 0, 1
	scale := byte(1)
	for n := 0; n < 16; n++ {
		discrepancy := s[n]
		for i := 1; i <= degree; i++ {
			discrepancy ^= gfMul(locator[i], s[n-i])
		}
		if discrepancy == 0 {
			shift++
			continue
		}
		old := locator
		factor := gfDiv(discrepancy, scale)
		for i := 0; i+shift < len(locator); i++ {
			locator[i+shift] ^= gfMul(factor, previous[i])
		}
		if 2*degree <= n {
			degree = n + 1 - degree
			previous = old
			scale = discrepancy
			shift = 1
		} else {
			shift++
		}
	}
	if degree == 0 || degree > 8 {
		return 0, errRSUncorrectable
	}
	// Chien search is restricted to transmitted positions; a root in the
	// shortened-away leading zero region must never be repaired as real data.
	var positions [8]int
	var locations [8]byte
	count := 0
	for pos := 0; pos < 204; pos++ {
		power := 203 - pos
		inverse := gfExp[(255-power)%255]
		value := locator[degree]
		for j := degree - 1; j >= 0; j-- {
			value = gfMul(value, inverse) ^ locator[j]
		}
		if value == 0 {
			if count == degree {
				return 0, errRSUncorrectable
			}
			positions[count] = pos
			locations[count] = gfExp[power]
			count++
		}
	}
	if count != degree {
		return 0, errRSUncorrectable
	}
	// Solve the small Vandermonde system S_j = sum(e_i*X_i^j). This is a
	// direct magnitude calculation and avoids Forney's root-offset convention.
	var matrix [8][9]byte
	for col := 0; col < count; col++ {
		power := byte(1)
		for row := 0; row < count; row++ {
			matrix[row][col] = power
			power = gfMul(power, locations[col])
		}
	}
	for row := 0; row < count; row++ {
		matrix[row][count] = s[row]
	}
	for col := 0; col < count; col++ {
		pivot := col
		for pivot < count && matrix[pivot][col] == 0 {
			pivot++
		}
		if pivot == count {
			return 0, errRSUncorrectable
		}
		matrix[col], matrix[pivot] = matrix[pivot], matrix[col]
		scale := matrix[col][col]
		for j := col; j <= count; j++ {
			matrix[col][j] = gfDiv(matrix[col][j], scale)
		}
		for row := 0; row < count; row++ {
			if row == col {
				continue
			}
			factor := matrix[row][col]
			for j := col; j <= count; j++ {
				matrix[row][j] ^= gfMul(factor, matrix[col][j])
			}
		}
	}
	var corrected [204]byte
	copy(corrected[:], word)
	for i := 0; i < count; i++ {
		if matrix[i][count] == 0 {
			return 0, errRSUncorrectable
		}
		corrected[positions[i]] ^= matrix[i][count]
	}
	if !allZero(syndromes(corrected[:])) {
		return 0, errRSUncorrectable
	}
	copy(word, corrected[:])
	return count, nil
}
