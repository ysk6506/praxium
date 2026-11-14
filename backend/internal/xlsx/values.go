package xlsx

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// plainNumber writes the text of a number of a cell in its plain decimal form: no exponent, no trailing zeros, no
// ".0" on an integer. It works on the digits of the text, so a number the file stored with more digits than a float
// keeps is not rounded again. ok is false when the text is not a number.
func plainNumber(s string) (string, bool) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	if neg || strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	mant, expText, hasExp := strings.Cut(strings.ToLower(s), "e")
	exp := 0
	if hasExp {
		n, err := strconv.Atoi(expText)
		if err != nil || n > 400 || n < -400 {
			return "", false
		}
		exp = n
	}
	whole, frac, _ := strings.Cut(mant, ".")
	if !digitsOnly(whole) || !digitsOnly(frac) || whole+frac == "" {
		return "", false
	}
	digits := whole + frac
	point := len(whole) + exp
	out := placePoint(digits, point)
	if neg && out != "0" {
		out = "-" + out
	}
	return out, true
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// placePoint puts the decimal point after point digits of a string of digits, filling with zeros where the digits run
// out, and drops what has no value.
func placePoint(digits string, point int) string {
	switch {
	case point <= 0:
		digits = strings.Repeat("0", -point+1) + digits
		point = 1
	case point > len(digits):
		digits += strings.Repeat("0", point-len(digits))
	}
	whole := strings.TrimLeft(digits[:point], "0")
	frac := strings.TrimRight(digits[point:], "0")
	if whole == "" {
		whole = "0"
	}
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}

// The kinds of a style that shows a number as a date or a time.
const (
	notDate  = 0
	dateOnly = 1
	dateTime = 2
	timeOnly = 3
)

// builtinDateKind is the date kind of the number formats Excel has built in.
func builtinDateKind(id int) int {
	switch {
	case id >= 14 && id <= 17, id >= 27 && id <= 31, id == 36, id == 50, id == 51, id == 54, id == 57, id == 58:
		return dateOnly
	case id >= 18 && id <= 21, id >= 32 && id <= 35, id >= 45 && id <= 47, id == 52, id == 53, id == 55, id == 56:
		return timeOnly
	case id == 22:
		return dateTime
	}
	return notDate
}

// formatDateKind reads the code of a number format and tells whether it shows a date, a date and a time, or a time.
func formatDateKind(code string) int {
	s := stripFormat(code)
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	s = strings.NewReplacer("am/pm", "", "a/p", "").Replace(s)
	hasHour := strings.ContainsAny(s, "h")
	hasSec := strings.Contains(s, "s")
	isDate := strings.ContainsAny(s, "yd") || (strings.Contains(s, "m") && !hasHour && !hasSec)
	switch {
	case isDate && (hasHour || hasSec):
		return dateTime
	case isDate:
		return dateOnly
	case hasHour || hasSec:
		return timeOnly
	}
	return notDate
}

// stripFormat lowers a format code and takes out what is no part of its shape: text in quotes, a character after a
// backslash, padding, and the brackets (a colour, a locale), except the brackets of an elapsed time, which keep their
// letter.
func stripFormat(code string) string {
	var b strings.Builder
	in := []rune(strings.ToLower(code))
	for i := 0; i < len(in); i++ {
		switch in[i] {
		case '"':
			for i++; i < len(in) && in[i] != '"'; i++ {
			}
		case '\\', '_', '*':
			i++
		case '[':
			j := i + 1
			for j < len(in) && in[j] != ']' {
				j++
			}
			if body := string(in[i+1 : min(j, len(in))]); body != "" && strings.Trim(body, "hms") == "" {
				b.WriteString(body)
			}
			i = j
		default:
			b.WriteRune(in[i])
		}
	}
	if strings.TrimSpace(b.String()) == "general" {
		return ""
	}
	return b.String()
}

// excelDate writes the number of a cell with a date style as a date. The serial counts days from the epoch of the
// workbook and the part after the point is the time of the day, which is rounded to the second. A date is
// "2006-01-02", a date with a time "2006-01-02T15:04:05Z" (a workbook has no time zone), a time "15:04:05". ok is
// false for a number that is no date (negative, past the year 9999).
func excelDate(serial float64, date1904 bool, kind int) (string, bool) {
	if serial < 0 || serial >= 2958466 || math.IsNaN(serial) {
		return "", false
	}
	total := int64(math.Round(serial * 86400))
	days, secs := total/86400, int(total%86400)
	clock := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(secs) * time.Second).Format("15:04:05")
	if kind == timeOnly {
		return pad2(total/3600) + clock[2:], true
	}
	day, ok := dayOf(days, date1904)
	if !ok {
		return "", false
	}
	if kind == dateOnly {
		return day, true
	}
	return day + "T" + clock + "Z", true
}

func pad2(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

// dayOf is the date of a day number of the workbook. The 1900 system counts a day 60 that never existed (the leap
// year bug that Excel keeps for compatibility); it is written as the date Excel shows.
func dayOf(days int64, date1904 bool) (string, bool) {
	if date1904 {
		return time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(days)).Format("2006-01-02"), true
	}
	switch {
	case days < 1:
		return "", false
	case days == 60:
		return "1900-02-29", true
	case days < 60:
		return time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(days)).Format("2006-01-02"), true
	}
	return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(days)).Format("2006-01-02"), true
}

// isoDate writes the text of a cell of the date type (t="d") the way excelDate does.
func isoDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "2006-01-02T15:04:05", "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05.999999999Z07:00", "15:04:05"} {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		switch {
		case layout == "2006-01-02":
			return t.Format("2006-01-02"), true
		case layout == "15:04:05":
			return t.Format("15:04:05"), true
		}
		return t.UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z"), true
	}
	return "", false
}

// unescapeXString turns the escapes of a string of a workbook back into characters: _x000D_ is a carriage return.
func unescapeXString(s string) string {
	if !strings.Contains(s, "_x") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if r, n, ok := xEscape(s[i:]); ok {
			b.WriteRune(r)
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// xEscape reads _xHHHH_ at the start of a string.
func xEscape(s string) (rune, int, bool) {
	if len(s) < 7 || s[0] != '_' || s[1] != 'x' || s[6] != '_' {
		return 0, 0, false
	}
	n, err := strconv.ParseUint(s[2:6], 16, 32)
	if err != nil {
		return 0, 0, false
	}
	return rune(n), 7, true
}
