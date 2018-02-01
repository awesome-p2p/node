package server

import (
	"github.com/ethereum/go-ethereum/accounts/keystore"
	identity_handler "github.com/mysterium/node/cmd/server/identity"
	"github.com/mysterium/node/communication"
	nats_dialog "github.com/mysterium/node/communication/nats/dialog"
	nats_discovery "github.com/mysterium/node/communication/nats/discovery"
	"github.com/mysterium/node/identity"
	"github.com/mysterium/node/ip"
	"github.com/mysterium/node/location"
	"github.com/mysterium/node/nat"
	"github.com/mysterium/node/openvpn"
	"github.com/mysterium/node/openvpn/middlewares/server/auth"
	openvpn_session "github.com/mysterium/node/openvpn/session"
	"github.com/mysterium/node/server"
	"github.com/mysterium/node/session"
	"path/filepath"
)

// NewCommand function creates new server command by given options
func NewCommand(options CommandOptions) *Command {
	return NewCommandWith(
		options,
		server.NewClient(),
		ip.NewResolver(),
		nat.NewService(),
	)
}

// NewCommandWith function creates new client command by given options + injects given dependencies
func NewCommandWith(
	options CommandOptions,
	mysteriumClient server.Client,
	ipResolver ip.Resolver,
	natService nat.NATService,
) *Command {

	keystoreInstance := keystore.NewKeyStore(options.DirectoryKeystore, keystore.StandardScryptN, keystore.StandardScryptP)
	cache := identity.NewIdentityCache(options.DirectoryKeystore, "remember.json")
	identityManager := identity.NewIdentityManager(keystoreInstance)
	createSigner := func(id identity.Identity) identity.Signer {
		return identity.NewSigner(keystoreInstance, id)
	}
	identityHandler := identity_handler.NewHandler(
		identityManager,
		mysteriumClient,
		cache,
		createSigner,
	)

	var locationDetector location.Detector
	if options.LocationCountry != "" {
		locationDetector = location.NewDetectorFake(options.LocationCountry)
	} else if options.LocationDatabase != "" {
		locationDetector = location.NewDetector(filepath.Join(options.DirectoryConfig, options.LocationDatabase))
	} else {
		locationDetector = location.NewDetectorFake("")
	}

	return &Command{
		identityLoader: func() (identity.Identity, error) {
			return identity_handler.LoadIdentity(identityHandler, options.NodeKey, options.Passphrase)
		},
		createSigner:     createSigner,
		locationDetector: locationDetector,
		ipResolver:       ipResolver,
		mysteriumClient:  mysteriumClient,
		natService:       natService,
		dialogWaiterFactory: func(myID identity.Identity) communication.DialogWaiter {
			return nats_dialog.NewDialogWaiter(
				nats_discovery.NewAddressGenerate(myID),
				identity.NewSigner(keystoreInstance, myID),
			)
		},
		sessionManagerFactory: func(vpnServerIP string) session.ManagerInterface {
			return openvpn_session.NewManager(openvpn.NewClientConfig(
				vpnServerIP,
				filepath.Join(options.DirectoryConfig, "ca.crt"),
				filepath.Join(options.DirectoryConfig, "ta.key"),
			))
		},
		vpnServerFactory: func() *openvpn.Server {
			vpnServerConfig := openvpn.NewServerConfig(
				"10.8.0.0", "255.255.255.0",
				filepath.Join(options.DirectoryConfig, "ca.crt"),
				filepath.Join(options.DirectoryConfig, "server.crt"),
				filepath.Join(options.DirectoryConfig, "server.key"),
				filepath.Join(options.DirectoryConfig, "dh.pem"),
				filepath.Join(options.DirectoryConfig, "crl.pem"),
				filepath.Join(options.DirectoryConfig, "ta.key"),
			)
			authenticator := auth.NewCheckerFake()
			vpnMiddlewares := []openvpn.ManagementMiddleware{
				auth.NewMiddleware(authenticator),
			}
			return openvpn.NewServer(vpnServerConfig, options.DirectoryRuntime, vpnMiddlewares...)
		},
	}
}
