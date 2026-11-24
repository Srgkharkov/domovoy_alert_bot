package bot

import "testing"

func TestChannelLabel(t *testing.T) {
	tests := []struct {
		device, channel string
		n, total        int
		want            string
	}{
		{"Кухня подсветка и свет", "переключатель 1", 1, 2, "Кухня подсветка и свет · 1"},
		{"Кухня подсветка и свет", "Channel 2", 2, 2, "Кухня подсветка и свет · 2"},
		{"Кухня", "Подсветка", 1, 2, "Кухня · Подсветка"},
		{"Умная розетка EU", "Розетка", 1, 1, "Розетка"},
		{"Умная розетка EU", "", 1, 1, "Умная розетка EU"},
	}
	for _, tt := range tests {
		if got := channelLabel(tt.device, tt.channel, tt.n, tt.total); got != tt.want {
			t.Errorf("channelLabel(%q, %q) = %q, want %q", tt.device, tt.channel, got, tt.want)
		}
	}
}
