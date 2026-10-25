package catalogrules

import "testing"

func TestUnofficialTagsFromName(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{
			name: "Aladdin (World) (Unl) (Pirate)",
			want: []string{"unlicensed", "pirate"},
		},
		{
			name: "Demo Game [Homebrew]",
			want: []string{"homebrew"},
		},
		{
			name: "Aftermarket Cart (Aftermarket)",
			want: []string{"aftermarket"},
		},
	}

	for _, tt := range tests {
		got := UnofficialTagsFromName(tt.name)
		if len(got) != len(tt.want) {
			t.Fatalf("UnofficialTagsFromName(%q) = %#v, want %#v", tt.name, got, tt.want)
		}
		for i := range tt.want {
			if got[i] != tt.want[i] {
				t.Fatalf("UnofficialTagsFromName(%q) = %#v, want %#v", tt.name, got, tt.want)
			}
		}
	}
}
