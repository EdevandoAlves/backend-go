package application

import "testing"

func TestOpenWalletCommandRequiresIdentity(t *testing.T) {
	command := OpenWalletCommand{}
	if command.WalletID != "" {
		t.Fatal("unexpected wallet id")
	}
}
