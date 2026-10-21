package ktf

import "github.com/movingwoo/wfeature/internal/armcore"

// A later title opens a socket while it starts, before it has anything to send.
// It asks BREW's network manager for one, keeps it, and loads its pictures only
// once it has it: a refused socket left the pictures unloaded, and its first
// frame read through one of them at zero. So the manager hands out a socket,
// and the socket refuses what a socket is for. No title reaches a network from
// this emulator (docs/network.md), and the refusal is the one a handset with
// no network gives: the connection fails because the network is down.
const (
	// nativeClassNetwork is BREW's AEECLSID_NET, the network manager.
	nativeClassNetwork = 0x1001105
	// nativeClassSocket is BREW's AEECLSID_SOCKET. Every socket the manager
	// opens is this one object, which is all sockets that never connect
	// need: nothing a title does with one has anything to keep apart.
	nativeClassSocket = 0x100102d
)

// The network manager's methods, by byte offset: INetMgr after INotifier's
// three.
const (
	// nativeNetworkLookUp is GetHostByName(result, name, callback), which a
	// title asks before it connects. It is never answered, as a lookup on a
	// handset out of coverage is not: the title that asks gives up on its own
	// about nine seconds later and says the server could not be reached.
	// Answering it would mean calling the title back from the platform,
	// which nothing else here needs.
	nativeNetworkLookUp     = 0x0c
	nativeNetworkOpenSocket = 0x14
)

// The socket's methods, by byte offset: ISocket after IAStream's five, which
// are the reference count, Readable, Read and Cancel.
const (
	nativeSocketRead = 0x0c
	// nativeSocketCancel drops a callback a title is waiting on, and no
	// socket here has one to drop.
	nativeSocketCancel    = 0x10
	nativeSocketLastError = 0x18
	nativeSocketConnect   = 0x1c
	nativeSocketWrite     = 0x24
)

// BREW's network answers: AEE_NET_ERROR, which a call returns when it failed,
// and AEE_NET_ENETDOWN, the error GetLastError then names.
const (
	nativeNetworkFailed = 0xffffffff
	nativeNetworkDown   = 0x216
)

// installNetwork registers the network manager and its socket.
func (platform *NativePlatform) installNetwork() {
	client := platform.client
	network := nativeInterfaceSurface(nativeClassNetwork)
	client.Serve(network, nativeNetworkLookUp, nativeAnswerZero)
	client.Serve(network, nativeNetworkOpenSocket, platform.openSocket)
	socket := nativeInterfaceSurface(nativeClassSocket)
	client.Serve(socket, nativeSocketCancel, nativeAnswerZero)
	client.Serve(socket, nativeSocketConnect, nativeAnswerNetworkFailed)
	client.Serve(socket, nativeSocketRead, nativeAnswerNetworkFailed)
	client.Serve(socket, nativeSocketWrite, nativeAnswerNetworkFailed)
	client.Serve(socket, nativeSocketLastError, func(*armcore.Thread) (uint32, error) { return nativeNetworkDown, nil })
}

// openSocket answers OpenSocket with the socket, whichever kind was asked for.
func (platform *NativePlatform) openSocket(*armcore.Thread) (uint32, error) {
	return platform.client.InterfaceObject(nativeClassSocket)
}

// nativeAnswerNetworkFailed refuses a socket call. Connect answers it at once
// rather than through its callback: BREW's Connect returns AEE_NET_ERROR for
// a connection it could not start, and the callback is only for one it did.
func nativeAnswerNetworkFailed(*armcore.Thread) (uint32, error) { return nativeNetworkFailed, nil }
