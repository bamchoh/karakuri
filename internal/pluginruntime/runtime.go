package pluginruntime

import (
	"errors"
	"fmt"
	"modbus_simulator/internal/domain/protocol"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
)

func RunFactory(factory protocol.ServerFactory, debug bool) error {
	plugin := newServer(factory)

	addr := "127.0.0.1:"
	if debug {
		addr += "50001"
	} else {
		addr += "0"
	}

	return run(plugin, addr)
}

func run(plugin plugin, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	port := lis.Addr().(*net.TCPAddr).Port

	grpcServer := grpc.NewServer()
	plugin.Register(grpcServer)

	serverErrCh := make(chan error, 1)

	go func() {
		err := grpcServer.Serve(lis)

		// GracefulStop() 後の正常終了は無視
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serverErrCh <- err
		}
		close(serverErrCh)
	}()

	fmt.Printf("GRPC_PORT=%d\n", port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		grpcServer.GracefulStop()
		return nil

	case err := <-serverErrCh:
		if err != nil {
			return fmt.Errorf("grpc server: %w", err)
		}
		return nil
	}
}
