package dotty

import "testing"

func TestParseSelector(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		want    Selector
		wantErr string
	}{
		{
			name: "package selector",
			arg:  "zsh",
			want: Selector{Package: "zsh"},
		},
		{
			name: "package source selector",
			arg:  "scripts/docx2pdf",
			want: Selector{Package: "scripts", Source: "docx2pdf"},
		},
		{
			name: "nested package source selector",
			arg:  "scripts/office/docx2pdf",
			want: Selector{Package: "scripts", Source: "office/docx2pdf"},
		},
		{
			name:    "empty source selector",
			arg:     "nvim/",
			wantErr: "empty source selector",
		},
		{
			name:    "empty selector",
			arg:     "",
			wantErr: "empty selector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSelector(tt.arg)
			if tt.wantErr != "" {
				requireErrorContains(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			if got != tt.want {
				t.Fatalf("selector mismatch: want %#v, got %#v", tt.want, got)
			}
			if tt.want.Source == "" && !got.IsPackage() {
				t.Fatalf("expected %#v to be a package selector", got)
			}
			if tt.want.Source != "" && !got.IsPackageSource() {
				t.Fatalf("expected %#v to be a package source selector", got)
			}
		})
	}
}
