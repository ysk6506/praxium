package worldgen

import "strconv"

// An object id is a short type prefix and the address of the object in the
// estate, such as "srv.3.1.4.7.12" for server 12 of rack 7 of row 4 of hall 1
// of site 3. Ids are computed, never looked up, so any part of the world can
// be made again on its own. Region and other site-less objects use site 0.
func mkid(prefix string, v ...int) string {
	var buf [48]byte
	b := append(buf[:0], prefix...)
	for _, n := range v {
		b = append(b, '.')
		b = strconv.AppendInt(b, int64(n), 10)
	}
	return string(b)
}

// siteOf reads the site number out of an id made by mkid ("" prefix, then
// the site as the first number). It returns -1 for an id of another shape.
func siteOf(id string) int {
	i := 0
	for i < len(id) && id[i] != '.' {
		i++
	}
	if i >= len(id) {
		return -1
	}
	j := i + 1
	n := 0
	for j < len(id) && id[j] >= '0' && id[j] <= '9' {
		n = n*10 + int(id[j]-'0')
		j++
	}
	if j == i+1 {
		return -1
	}
	return n
}

const hexDigits = "0123456789abcdef"

func appendHex64(b []byte, v uint64) []byte {
	for shift := 60; shift >= 0; shift -= 4 {
		b = append(b, hexDigits[(v>>uint(shift))&0xf])
	}
	return b
}

// linkID names a link by its type and both ends (96 bits, 24 hex digits).
func linkID(lt int, from, to string) string {
	h1 := mix(hash64(from), uint64(lt)+1)
	h2 := mix(hash64(to), h1)
	h3 := mix(h1, h2)
	var buf [40]byte
	b := append(buf[:0], 'l')
	b = appendHex64(b, h2)
	b = appendHex64(b, h3>>32<<32)[:25]
	return string(b)
}
