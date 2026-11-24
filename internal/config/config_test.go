package config

import (
	"reflect"
	"testing"
)

func TestParseInt64List(t *testing.T) {
	tests := []struct {
		in      string
		want    []int64
		wantErr bool
	}{
		{in: "", want: nil},
		{in: "-1001234567890", want: []int64{-1001234567890}},
		{in: " -100123 , 42 ,", want: []int64{-100123, 42}},
		{in: "abc", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseInt64List(tt.in)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parseInt64List(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseInt64List(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestValidateBot(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateBot(); err == nil {
		t.Fatal("expected error for empty TELEGRAM_BOT_TOKEN")
	}
}
