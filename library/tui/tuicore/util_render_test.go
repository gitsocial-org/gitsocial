// util_render_test.go - Tests for rendering utilities and math conversion
package tuicore

import (
	"strings"
	"testing"
	"time"
)

// FormatTime and FormatFullTime name the same day for a value from another zone.
func TestFormatTimeAgreesWithFullTimeOnTheDate(t *testing.T) {
	_, localOffset := time.Now().Zone()
	behind := time.FixedZone("behind", localOffset-7*3600)
	value := time.Date(2020, 1, 1, 23, 30, 0, 0, behind)
	day := value.Local().Format("Jan 2, 2006")
	if got := FormatTime(value); got != day {
		t.Errorf("FormatTime = %q, want %q", got, day)
	}
	if got := FormatFullTime(value); !strings.HasPrefix(got, day) {
		t.Errorf("FormatFullTime = %q, want it to start with %q", got, day)
	}
}

func TestRenderMath_greekLetters(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`$\alpha$`, "\u03B1"},
		{`$\beta$`, "\u03B2"},
		{`$\pi$`, "\u03C0"},
		{`$\Omega$`, "\u03A9"},
		{`$\alpha + \beta$`, "\u03B1 + \u03B2"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := renderMath(tt.input)
			if got != tt.want {
				t.Errorf("renderMath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRenderMath_operators(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`$\pm$`, "\u00B1"},
		{`$\times$`, "\u00D7"},
		{`$\div$`, "\u00F7"},
		{`$\infty$`, "\u221E"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := renderMath(tt.input)
			if got != tt.want {
				t.Errorf("renderMath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRenderMath_relations(t *testing.T) {
	got := renderMath(`$\leq$`)
	if got != "\u2264" {
		t.Errorf("renderMath(leq) = %q, want %q", got, "\u2264")
	}
}

func TestRenderMath_blockMath(t *testing.T) {
	got := renderMath(`$$\alpha + \beta$$`)
	if got != "\u03B1 + \u03B2" {
		t.Errorf("renderMath(block) = %q", got)
	}
}

func TestRenderMath_noMath(t *testing.T) {
	input := "Just regular text"
	got := renderMath(input)
	if got != input {
		t.Errorf("renderMath(no math) = %q, want %q", got, input)
	}
}

func TestConvertFrac(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", `\frac{a}{b}`, "(a)/(b)"},
		{"numbers", `\frac{1}{2}`, "(1)/(2)"},
		{"nested", `\frac{\alpha}{2}`, "(\u03B1)/(2)"},
		{"no frac", "x + y", "x + y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertFrac(tt.input)
			if got != tt.want {
				t.Errorf("convertFrac(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConvertSqrt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", `\sqrt{x}`, "\u221A(x)"},
		{"number", `\sqrt{4}`, "\u221A(4)"},
		{"no sqrt", "x + y", "x + y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertSqrt(tt.input)
			if got != tt.want {
				t.Errorf("convertSqrt(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConvertBinom(t *testing.T) {
	got := convertBinom(`\binom{n}{k}`)
	if got != "C(n,k)" {
		t.Errorf("convertBinom() = %q, want %q", got, "C(n,k)")
	}
}

func TestConvertVec(t *testing.T) {
	got := convertVec(`\vec{v}`)
	if got != "v\u20D7" {
		t.Errorf("convertVec() = %q, want %q", got, "v\u20D7")
	}
}

func TestConvertMatrix(t *testing.T) {
	input := `\begin{matrix}a & b \\ c & d\end{matrix}`
	got := convertMatrix(input)
	if got != "[a b; c d]" {
		t.Errorf("convertMatrix() = %q, want %q", got, "[a b; c d]")
	}
}

func TestConvertSuperscripts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"single digit", "x^2", "x\u00B2"},
		{"braced", "x^{23}", "x\u00B2\u00B3"},
		{"n", "x^n", "x\u207F"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertSuperscripts(tt.input)
			if got != tt.want {
				t.Errorf("convertSuperscripts(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConvertSubscripts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"single digit", "x_0", "x\u2080"},
		{"braced", "x_{12}", "x\u2081\u2082"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertSubscripts(tt.input)
			if got != tt.want {
				t.Errorf("convertSubscripts(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractBraceContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		pos      int
		wantText string
		wantEnd  int
	}{
		{"simple", "{hello}", 0, "hello", 7},
		{"nested", "{a{b}c}", 0, "a{b}c", 7},
		{"not brace", "hello", 0, "", -1},
		{"past end", "{hello}", 10, "", -1},
		{"empty", "{}", 0, "", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, end := extractBraceContent(tt.input, tt.pos)
			if text != tt.wantText {
				t.Errorf("text = %q, want %q", text, tt.wantText)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %d, want %d", end, tt.wantEnd)
			}
		})
	}
}

func TestRenderMathWithPlaceholders(t *testing.T) {
	content, extracted := renderMathWithPlaceholders("Text $\\alpha$ more")
	if len(extracted) != 1 {
		t.Fatalf("len(extracted) = %d, want 1", len(extracted))
	}
	if !strings.Contains(content, mathPlaceholderPrefix) {
		t.Error("content should contain placeholder")
	}
}

func TestRestoreMath(t *testing.T) {
	content := "\x00MATH0\x00"
	extracted := []string{"\x01hello\x01"}
	got := restoreMath(content, extracted)
	if got != "hello" {
		t.Errorf("restoreMath() = %q, want %q", got, "hello")
	}
}

func TestRenderMath_arrows(t *testing.T) {
	got := renderMath(`$\rightarrow$`)
	if got != "\u2192" {
		t.Errorf("renderMath(rightarrow) = %q, want %q", got, "\u2192")
	}
}

func TestRenderMath_functions(t *testing.T) {
	got := renderMath(`$\sin$`)
	if got != "sin" {
		t.Errorf("renderMath(sin) = %q, want %q", got, "sin")
	}
}
