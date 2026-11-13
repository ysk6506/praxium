package worldgen

import (
	"strconv"
	"strings"
	"time"
)

// Names of invented manufacturers and models. They are made up and match no
// real product line.
var (
	serverMakers = []string{"Zorvane", "Kelmar", "Tibrix"}
	netMakers    = []string{"Quenlo", "Varsek", "Brindle"}
	facMakers    = []string{"Hollin", "Tarsk", "Orvane"}
	storMakers   = []string{"Daskel", "Mirren"}
)

const b32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// base32 writes n characters of a 64-bit number (the top bits first).
func base32(h uint64, n int) string {
	var buf [13]byte
	for i := 0; i < n; i++ {
		shift := uint(59 - 5*i)
		if i >= 12 {
			shift = 0
		}
		buf[i] = b32[(h>>shift)&31]
	}
	return string(buf[:n])
}

// dateTable holds the dates the generic time values pick from.
func dateTable() []string {
	out := make([]string, 1500)
	t := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = t.AddDate(0, 0, i).Format("2006-01-02T15:04:05Z")
	}
	return out
}

// enumBias is the value most objects have for these properties.
var enumBias = map[string]string{
	"lifecycle_state": "active", "oper_state": "up", "admin_state": "up", "power_state": "on",
	"maintenance_state": "normal", "connection_state": "connected", "guest_tools_state": "running",
}

type valueFn func(g *gen, ps *propSpec, h uint64, d *draw) val

var valueFns = [...]valueFn{
	hString: vString, hIPv4: vIPv4, hMAC: vMAC, hSerial: vSerial, hFQDN: vFQDN, hUUID: vUUID,
	hInt: vInt, hNumber: vNumber, hBool: vBool, hTime: vTime, hEnum: vEnum, hQty: vQty,
	hList: vList, hMap: vMap, hSkip: vNone,
}

func vNone(*gen, *propSpec, uint64, *draw) val { return val{} }

func vString(_ *gen, ps *propSpec, h uint64, _ *draw) val {
	if ps.keyPart {
		return strVal(ps.name + "-" + base32(h, 13))
	}
	return strVal(ps.name + "-" + base32(h, 6))
}

func vIPv4(_ *gen, _ *propSpec, h uint64, _ *draw) val {
	return strVal("10." + strconv.Itoa(int(h>>40&255)) + "." + strconv.Itoa(int(h>>32&255)) + "." + strconv.Itoa(1+int(h>>24&253)))
}

func vMAC(_ *gen, _ *propSpec, h uint64, _ *draw) val {
	b := make([]byte, 0, 12)
	first := byte(h>>58&0x3f)<<2 | 0x02
	b = append(b, hexDigits[first>>4], hexDigits[first&15])
	for i := 0; i < 5; i++ {
		c := byte(h >> uint(8*i))
		b = append(b, hexDigits[c>>4], hexDigits[c&15])
	}
	return strVal(string(b))
}

func vSerial(_ *gen, _ *propSpec, h uint64, _ *draw) val { return strVal("SN" + base32(h, 12)) }

func vFQDN(_ *gen, _ *propSpec, h uint64, _ *draw) val {
	return strVal("h" + strings.ToLower(base32(h, 10)) + ".dc.example.test")
}

func vUUID(_ *gen, _ *propSpec, h uint64, _ *draw) val {
	h2 := mix(h, 0x51ed270b0)
	b := make([]byte, 0, 36)
	b = appendHex64(b, h)[:8]
	b = append(b, '-')
	b = appendHex64(b, h2)[:4]
	b = append(b, '-', '4')
	b = appendHex64(b, h2<<16)[:3]
	b = append(b, '-', '8')
	b = appendHex64(b, h2<<28)[:3]
	b = append(b, '-')
	b = appendHex64(b, mix(h, 7))[:12]
	return strVal(string(b))
}

func vInt(_ *gen, ps *propSpec, _ uint64, d *draw) val { return intVal(d.between(ps.lo, ps.hi)) }

func vNumber(_ *gen, ps *propSpec, _ uint64, d *draw) val {
	return decVal(d.between(ps.lo, ps.hi)*10 + d.intn(10))
}

func vBool(_ *gen, _ *propSpec, _ uint64, d *draw) val { return boolVal(d.intn(2) == 0) }

func vTime(g *gen, _ *propSpec, _ uint64, d *draw) val { return strVal(g.dates[d.intn(len(g.dates))]) }

func vEnum(_ *gen, ps *propSpec, _ uint64, d *draw) val {
	if len(ps.enum) == 0 {
		return strVal("")
	}
	if want, ok := enumBias[ps.name]; ok && d.chance(0.93) {
		for _, e := range ps.enum {
			if e == want {
				return strVal(e)
			}
		}
	}
	return strVal(ps.enum[d.intn(len(ps.enum))])
}

func vQty(_ *gen, ps *propSpec, _ uint64, d *draw) val {
	if d.intn(3) == 0 {
		return decVal(d.between(ps.lo, ps.hi)*10 + d.intn(10))
	}
	return intVal(d.between(ps.lo, ps.hi))
}

func vList(_ *gen, ps *propSpec, h uint64, d *draw) val {
	if ps.name == "hostnames" {
		return litVal(`["h` + base32(h, 10) + `.dc.example.test"]`)
	}
	n := 1 + d.intn(2)
	b := []byte{'['}
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendJSONString(b, "item-"+base32(mix(h, uint64(i)), 5))
	}
	return litVal(string(append(b, ']')))
}

func vMap(_ *gen, ps *propSpec, h uint64, _ *draw) val {
	return litVal(`{"` + ps.name + `":"v-` + base32(h, 5) + `"}`)
}

// genValue makes the value of one property from the hash of the object and
// the object's draw. Same inputs, same value.
func (g *gen) genValue(ps *propSpec, idx int, h uint64, d *draw) val {
	return valueFns[ps.hint](g, ps, mix(h, uint64(idx)+1), d)
}
