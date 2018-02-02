package client_connection

import (
	"errors"
	"github.com/mysterium/node/communication"
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

type DialogEstablisherFactory func(identity identity.Identity) communication.DialogEstablisher

type VpnClientFactory func(session.SessionDto, identity.Identity, state.ClientStateCallback) (openvpn.Client, error)

type connectionManager struct {
	//these are passed on creation
	mysteriumClient  server.Client
	newDialogCreator DialogEstablisherFactory
	newVpnClient     VpnClientFactory
	statsKeeper      bytescount.SessionStatsKeeper
	//these are populated by Connect at runtime
	dialog         communication.Dialog
	vpnClient      openvpn.Client
	status         ConnectionStatus
	currentSession session.SessionID
}

func NewManager(mysteriumClient server.Client, dialogEstablisherFactory DialogEstablisherFactory,
	vpnClientFactory VpnClientFactory, statsKeeper bytescount.SessionStatsKeeper) *connectionManager {
	return &connectionManager{
		mysteriumClient:  mysteriumClient,
		newDialogCreator: dialogEstablisherFactory,
		newVpnClient:     vpnClientFactory,
		statsKeeper:      statsKeeper,
		dialog:           nil,
		vpnClient:        nil,
		status:           statusNotConnected(),
	}
}

func (manager *connectionManager) Connect(myID identity.Identity, nodeKey string) error {
	manager.status = statusConnecting()

	providerID := identity.FromAddress(nodeKey)

	proposals, err := manager.mysteriumClient.FindProposals(nodeKey)
	if err != nil {
		manager.status = statusError(err)
		return err
	}
	if len(proposals) == 0 {
		err = errors.New("node has no service proposals")
		manager.status = statusError(err)
		return err
	}
	proposal := proposals[0]

	dialogCreator := manager.newDialogCreator(myID)
	manager.dialog, err = dialogCreator.CreateDialog(providerID, proposal.ProviderContacts[0])
	if err != nil {
		manager.status = statusError(err)
		return err
	}

	vpnSession, err := session.RequestSessionCreate(manager.dialog, proposal.ID)
	if err != nil {
		manager.status = statusError(err)
		return err
	}
	manager.currentSession = vpnSession.ID

	manager.vpnClient, err = manager.newVpnClient(*vpnSession, myID, manager.onVpnStateChanged)

	if err := manager.vpnClient.Start(); err != nil {
		manager.status = statusError(err)
		return err
	}

	return nil
}

func (manager *connectionManager) Status() ConnectionStatus {
	return manager.status
}

func (manager *connectionManager) Disconnect() error {
	manager.status = statusDisconnecting()

	if manager.vpnClient != nil {
		if err := manager.vpnClient.Stop(); err != nil {
			return err
		}
	}
	if manager.dialog != nil {
		if err := manager.dialog.Close(); err != nil {
			return err
		}
	}

	return nil
}

func (manager *connectionManager) Wait() error {
	return manager.vpnClient.Wait()
}

func (manager *connectionManager) onVpnStateChanged(state openvpn.State) {
	switch state {
	case openvpn.STATE_CONNECTED:
		manager.statsKeeper.MarkSessionStart()
		manager.status = statusConnected(manager.currentSession)
	case openvpn.STATE_RECONNECTING:
		manager.status = statusConnecting()
	case openvpn.STATE_EXITING:
		manager.status = statusNotConnected()
	}
}

func statusError(err error) ConnectionStatus {
	return ConnectionStatus{NotConnected, "", err}
}

func statusConnecting() ConnectionStatus {
	return ConnectionStatus{Connecting, "", nil}
}

func statusConnected(sessionID session.SessionID) ConnectionStatus {
	return ConnectionStatus{Connected, sessionID, nil}
}

func statusNotConnected() ConnectionStatus {
	return ConnectionStatus{NotConnected, "", nil}
}

func statusDisconnecting() ConnectionStatus {
	return ConnectionStatus{Disconnecting, "", nil}
}

func ConfigureVpnClientFactory(mysteriumAPIClient server.Client, vpnClientRuntimeDirectory string,
	signerFactory identity.SignerFactory, statsKeeper bytescount.SessionStatsKeeper) VpnClientFactory {
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
