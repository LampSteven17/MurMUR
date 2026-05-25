package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile_InlineComments(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"KEY1", "value1"},                    // ` # comment` stripped
		{"KEY2", "value2-no-comment"},         // no `#` to strip
		{"KEY3", "value with # inside"},       // double-quoted: literal `#` preserved
		{"KEY4", "value-with-#-no-space"},     // unquoted but no space before `#` → not a comment
		{"KEY5", "quoted # value"},            // single-quoted: literal `#` preserved
		{"KEY6", "trailing-tab-comment"},      // tab+# also counts as inline comment
	}

	src := "" +
		"KEY1=value1 # inline comment\n" +
		"KEY2=value2-no-comment\n" +
		"KEY3=\"value with # inside\"  # outside comment stripped\n" +
		"KEY4=value-with-#-no-space\n" +
		"KEY5='quoted # value'\n" +
		"KEY6=trailing-tab-comment\t# tab before hash\n" +
		"# whole-line comment\n"

	path := filepath.Join(t.TempDir(), "test.env")
	if err := os.WriteFile(path, []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		os.Unsetenv(c.key)
	}
	defer func() {
		for _, c := range cases {
			os.Unsetenv(c.key)
		}
	}()

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	for _, c := range cases {
		if got := os.Getenv(c.key); got != c.want {
			t.Errorf("%s: got %q, want %q", c.key, got, c.want)
		}
	}
}
