package identity

import (
	"testing"

	"github.com/mysterium/node/identity"
	"github.com/mysterium/node/server"
	"github.com/stretchr/testify/assert"
)

var fakeSignerFactory = func(id identity.Identity) identity.Signer {
	return &fakeSigner{}
}
var existingIdentity = identity.Identity{"existing"}
var newIdentity = identity.Identity{"new"}

func TestUseExistingSucceeds(t *testing.T) {
	identityManager := identity.NewIdentityManagerFake([]identity.Identity{existingIdentity}, newIdentity)
	client := server.NewClientFake()
	cache := identity.NewIdentityCacheFake()

	handler := NewHandler(identityManager, client, cache, fakeSignerFactory)

	id, err := handler.UseExisting(existingIdentity.Address, "pass")
	assert.Equal(t, existingIdentity, id)
	assert.Nil(t, err)
	assert.Equal(t, "", client.RegisteredIdentity.Address)

	assert.Equal(t, existingIdentity.Address, identityManager.LastUnlockAddress)
	assert.Equal(t, "pass", identityManager.LastUnlockPassphrase)
}

func TestUseExistingFailsWhenUnlockFails(t *testing.T) {
	identityManager := identity.NewIdentityManagerFake([]identity.Identity{existingIdentity}, newIdentity)
	identityManager.MarkUnlockToFail()
	client := server.NewClientFake()
	cache := identity.NewIdentityCacheFake()

	handler := NewHandler(identityManager, client, cache, fakeSignerFactory)

	_, err := handler.UseExisting(existingIdentity.Address, "pass")
	assert.Error(t, err)

	assert.Equal(t, existingIdentity.Address, identityManager.LastUnlockAddress)
	assert.Equal(t, "pass", identityManager.LastUnlockPassphrase)
}

func TestUseFailsWhenIdentityNotFound(t *testing.T) {
	identityManager := identity.NewIdentityManagerFake([]identity.Identity{existingIdentity}, newIdentity)
	client := server.NewClientFake()
	cache := identity.NewIdentityCacheFake()

	handler := NewHandler(identityManager, client, cache, fakeSignerFactory)

	_, err := handler.UseExisting("does-not-exist", "pass")
	assert.NotNil(t, err)
}

func TestUseLastSucceeds(t *testing.T) {
	identityManager := identity.NewIdentityManagerFake([]identity.Identity{existingIdentity}, newIdentity)
	client := server.NewClientFake()
	cache := identity.NewIdentityCacheFake()

	fakeIdentity := identity.FromAddress("abc")
	cache.StoreIdentity(fakeIdentity)

	handler := NewHandler(identityManager, client, cache, fakeSignerFactory)

	id, err := handler.UseLast("pass")
	assert.Equal(t, fakeIdentity, id)
	assert.Nil(t, err)

	assert.Equal(t, "", client.RegisteredIdentity.Address)

	assert.Equal(t, "abc", identityManager.LastUnlockAddress)
	assert.Equal(t, "pass", identityManager.LastUnlockPassphrase)
}

func TestUseLastFailsWhenUnlockFails(t *testing.T) {
	identityManager := identity.NewIdentityManagerFake([]identity.Identity{existingIdentity}, newIdentity)
	identityManager.MarkUnlockToFail()
	client := server.NewClientFake()
	cache := identity.NewIdentityCacheFake()

	fakeIdentity := identity.FromAddress("abc")
	cache.StoreIdentity(fakeIdentity)

	handler := NewHandler(identityManager, client, cache, fakeSignerFactory)

	_, err := handler.UseLast("pass")
	assert.Error(t, err)

	assert.Equal(t, "", client.RegisteredIdentity.Address)

	assert.Equal(t, "abc", identityManager.LastUnlockAddress)
	assert.Equal(t, "pass", identityManager.LastUnlockPassphrase)
}

func TestUseNewSucceeds(t *testing.T) {
	identityManager := identity.NewIdentityManagerFake([]identity.Identity{existingIdentity}, newIdentity)
	client := server.NewClientFake()
	cache := identity.NewIdentityCacheFake()

	handler := NewHandler(identityManager, client, cache, fakeSignerFactory)

	id, err := handler.UseNew("pass")
	assert.Equal(t, newIdentity, id)
	assert.Nil(t, err)

	assert.Equal(t, newIdentity, client.RegisteredIdentity)

	assert.Equal(t, newIdentity.Address, identityManager.LastUnlockAddress)
	assert.Equal(t, "pass", identityManager.LastUnlockPassphrase)
}

func TestUseNewFailsWhenUnlockFails(t *testing.T) {
	identityManager := identity.NewIdentityManagerFake([]identity.Identity{existingIdentity}, newIdentity)
	identityManager.MarkUnlockToFail()
	client := server.NewClientFake()
	cache := identity.NewIdentityCacheFake()

	handler := NewHandler(identityManager, client, cache, fakeSignerFactory)

	_, err := handler.UseNew("pass")
	assert.Error(t, err)

	assert.Equal(t, newIdentity.Address, identityManager.LastUnlockAddress)
	assert.Equal(t, "pass", identityManager.LastUnlockPassphrase)
}

type fakeSigner struct {
}

func (fs *fakeSigner) Sign(message []byte) (identity.Signature, error) {
	return identity.SignatureBase64("deadbeef"), nil
}
