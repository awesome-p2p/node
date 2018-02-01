package client_connection

import (
	"errors"
	"github.com/mysterium/node/communication"
	"github.com/mysterium/node/identity"
	"github.com/mysterium/node/openvpn"
	"github.com/mysterium/node/openvpn/middlewares/client/bytescount"
	"github.com/mysterium/node/server"
	"github.com/mysterium/node/session"
)

type connectionManager struct {
	//these are passed on creation
	mysteriumClient  server.Client
	newDialogCreator DialogEstablisherCreator
	newVpnClient     VpnClientCreator
	statsKeeper      bytescount.SessionStatsKeeper
	//these are populated by Connect at runtime
	dialog         communication.Dialog
	vpnClient      openvpn.Client
	status         ConnectionStatus
	currentSession session.SessionID
}

func NewManager(mysteriumClient server.Client, dialogEstablisherFactory DialogEstablisherCreator,
	vpnClientFactory VpnClientCreator, statsKeeper bytescount.SessionStatsKeeper) *connectionManager {
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
