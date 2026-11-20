package proto

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckJSON_AcceptsOneObject(t *testing.T) {
	for name, in := range map[string]string{
		"empty object":         `{}`,
		"nested":               `{"a":{"b":[1,{"c":2}],"d":"x"}}`,
		"same key in siblings": `{"a":{"k":1},"b":{"k":2},"c":[{"k":1},{"k":2}]}`,
		"whitespace around":    " \n{\"a\":1}\n ",
	} {
		t.Run(name, func(t *testing.T) { require.NoError(t, checkJSON([]byte(in))) })
	}
}

func TestCheckJSON_Refusals(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxDepth+1) + `1` + strings.Repeat(`}`, maxDepth+1)
	for name, in := range map[string]string{
		"duplicate at the top":        `{"a":1,"a":2}`,
		"duplicate when nested":       `{"x":{"a":1,"b":2,"a":3}}`,
		"duplicate inside an array":   `{"x":[{"a":1,"a":2}]}`,
		"duplicate with escaped name": `{"a":1,"a":2}`,
		"not valid UTF-8":             "{\"a\":\"\xff\"}",
		"byte-order mark":             "\xef\xbb\xbf{}",
		"an array":                    `[1]`,
		"a bare string":               `"x"`,
		"a number":                    `1`,
		"empty input":                 ``,
		"two objects":                 `{}{}`,
		"trailing garbage":            `{} x`,
		"trailing comma":              `{"a":1,}`,
		"truncated":                   `{"a":`,
		"unterminated string":         `{"a":"b`,
		"single quotes":               `{'a':1}`,
		"nested too deep":             deep,
	} {
		t.Run(name, func(t *testing.T) {
			err := checkJSON([]byte(in))

			require.Error(t, err)
			assert.Equal(t, CodeProtocol, CodeOf(err), "%v", err)
		})
	}
}

func TestCheckJSON_NamesTheDuplicateMember(t *testing.T) {
	err := checkJSON([]byte(`{"price":"1","price":"2"}`))

	assert.Contains(t, err.Error(), `"price"`)
}

func TestParseTime(t *testing.T) {
	got, err := parseTime("2024-03-01T11:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 3, 1, 11, 0, 0, 0, time.UTC), got)
	assert.Equal(t, "2024-03-01T11:00:00Z", formatTime(got))

	for _, bad := range []string{
		"", "2024-03-01", "2024-03-01T11:00:00", "2024-03-01T11:00:00+00:00", "2024-03-01T12:00:00+01:00",
		"2024-03-01T11:00:00.5Z", "2024-03-01T11:00:00.000Z", "2024-03-01 11:00:00Z", "2024-02-30T11:00:00Z", "2024-03-01t11:00:00z",
	} {
		_, err := parseTime(bad)
		assert.Error(t, err, "%q", bad)
	}
}

func TestFormatTime_ConvertsToUTC(t *testing.T) {
	ny := time.FixedZone("NY", -5*3600)

	assert.Equal(t, "2024-03-01T11:00:00Z", formatTime(time.Date(2024, 3, 1, 6, 0, 0, 0, ny)))
}

func TestParseDecimal(t *testing.T) {
	for in, want := range map[string]string{
		"1.08764":      "1.08764",
		"1":            "1",
		"0":            "0",
		"0.5":          "0.5",
		"150.12345678": "150.12345678",
		"007":          "7",
	} {
		got, err := parseDecimal(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got.String(), in)
	}
	for _, bad := range []string{"", ".5", "5.", "-1", "+1", "1e3", "1E-3", "1,5", " 1", "1 ", "1.123456789", "0x10", "NaN", "1.0.0", "１"} {
		_, err := parseDecimal(bad)
		assert.Error(t, err, "%q", bad)
	}
}

func TestObj_Accessors(t *testing.T) {
	root, err := newObj([]byte(`{"s":"x","n":12,"b":true,"t":"2024-03-01T11:00:00Z","d":"1.5","a":["p","q"],"o":{"k":"v"},"null":null,"neg":-1,"frac":1.5,"exp":1e2}`), "m", CodeProtocol)
	require.NoError(t, err)

	s, ok, err := root.str("s", true)
	assert.Equal(t, "x", s)
	assert.True(t, ok)
	assert.NoError(t, err)
	n, _, err := root.uint("n", true)
	assert.Equal(t, uint64(12), n)
	assert.NoError(t, err)
	b, _, err := root.boolean("b", true)
	assert.True(t, b)
	assert.NoError(t, err)
	tm, _, err := root.timestamp("t", true)
	assert.Equal(t, time.Date(2024, 3, 1, 11, 0, 0, 0, time.UTC), tm)
	assert.NoError(t, err)
	d, _, err := root.decimal("d", true)
	assert.Equal(t, "1.5", d.String())
	assert.NoError(t, err)
	ss, _, err := root.strings("a", true)
	assert.Equal(t, []string{"p", "q"}, ss)
	assert.NoError(t, err)
	sub, _, err := root.object("o", true, CodeRejected)
	assert.NoError(t, err)
	assert.Equal(t, CodeRejected, sub.code)

	_, ok, err = root.str("absent", false)
	assert.False(t, ok)
	assert.NoError(t, err, "an absent optional member is fine")

	for name, call := range map[string]func() error{
		"missing required":       func() error { _, _, e := root.str("absent", true); return e },
		"null string":            func() error { _, _, e := root.str("null", false); return e },
		"null of any kind":       func() error { _, _, e := root.uint("null", false); return e },
		"number as string":       func() error { _, _, e := root.str("n", true); return e },
		"string as number":       func() error { _, _, e := root.uint("s", true); return e },
		"negative integer":       func() error { _, _, e := root.uint("neg", true); return e },
		"fractional integer":     func() error { _, _, e := root.uint("frac", true); return e },
		"exponent integer":       func() error { _, _, e := root.uint("exp", true); return e },
		"string as bool":         func() error { _, _, e := root.boolean("s", true); return e },
		"bad timestamp":          func() error { _, _, e := root.timestamp("s", true); return e },
		"number as decimal":      func() error { _, _, e := root.decimal("n", true); return e },
		"object as array":        func() error { _, _, e := root.array("o", true); return e },
		"array as object":        func() error { _, _, e := root.object("a", true, CodeProtocol); return e },
		"number in string array": func() error { _, _, e := root.strings("n", true); return e },
	} {
		err := call()
		require.Error(t, err, name)
		assert.Equal(t, CodeProtocol, CodeOf(err), name)
		assert.Contains(t, err.Error(), "m.", name, "the error says where")
	}
}

func TestObj_OnlyNamesTheFirstUnknownMemberInNameOrder(t *testing.T) {
	root, err := newObj([]byte(`{"b":1,"a":2,"ok":3}`), "run", CodeRejected)
	require.NoError(t, err)

	err = root.only("ok")

	assert.Equal(t, CodeRejected, CodeOf(err))
	assert.Contains(t, err.Error(), `"a"`)
	assert.NoError(t, root.only("a", "b", "ok"))
}

func TestNewObj_RefusesAnythingButAnObject(t *testing.T) {
	for _, in := range []string{``, `[]`, `"x"`, `1`, `null`, `{`} {
		_, err := newObj([]byte(in), "x", CodeInvalidOrder)
		require.Error(t, err, in)
		assert.Equal(t, CodeInvalidOrder, CodeOf(err))
	}
}

func TestCodeOf_AndValid(t *testing.T) {
	assert.Equal(t, CodeVersion, CodeOf(errorf(CodeVersion, "x")))
	assert.Equal(t, CodeInternal, CodeOf(assert.AnError))
	for _, c := range []Code{CodeProtocol, CodeVersion, CodeRejected, CodeInvalidOrder, CodeTimeout, CodeCancelled, CodeInternal} {
		assert.True(t, c.Valid(), c)
	}
	assert.False(t, Code("nope").Valid())
	assert.False(t, Code("").Valid())
}
