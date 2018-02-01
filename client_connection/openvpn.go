package client_connection

import (
	"github.com/mysterium/node/identity"
	"github.com/mysterium/node/openvpn"
	"github.com/mysterium/node/openvpn/middlewares/client/auth"
	"github.com/mysterium/node/openvpn/middlewares/client/bytescount"
	"github.com/mysterium/node/openvpn/middlewares/client/state"
	openvpnSession "github.com/mysterium/node/openvpn/session"
	"github.com/mysterium/node/server"
	"github.com/mysterium/node/session"
	"path/filepath"
	"time"
)

func ConfigureVpnClientFactory(mysteriumAPIClient server.Client, vpnClientRuntimeDirectory string,
	signerFactory identity.SignerFactory, statsKeeper bytescount.SessionStatsKeeper) VpnClientCreator {
	return func(vpnSession session.SessionDto, id identity.Identity, stateCallback state.ClientStateCallback) (openvpn.Client, error) {
		vpnConfig, err := openvpn.NewClientConfigFromString(
			vpnSession.Config,
			filepath.Join(vpnClientRuntimeDirectory, "client.ovpn"),
		)
		if err != nil {
			return nil, err
		}

		signer := signerFactory(id)

		statsSaver := bytescount.NewSessionStatsSaver(statsKeeper)
		statsSender := bytescount.NewSessionStatsSender(mysteriumAPIClient, vpnSession.ID, signer)
		statsHandler := bytescount.NewCompositeStatsHandler(statsSaver, statsSender)

		credentialsProvider := openvpnSession.SignatureCredentialsProvider(vpnSession.ID, signer)
		vpnMiddlewares := []openvpn.ManagementMiddleware{
			state.NewMiddleware(stateCallback),
			bytescount.NewMiddleware(statsHandler, 1*time.Minute),
			auth.NewMiddleware(credentialsProvider),
		}
		return openvpn.NewClient(
			vpnConfig,
			vpnClientRuntimeDirectory,
			vpnMiddlewares...,
		), nil
	}
}
