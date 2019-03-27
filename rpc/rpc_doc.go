// Package rpc implements zrepl daemon-to-daemon RPC protocol
// on top of a transport provided by package transport.
// The zrepl documentation refers to the client as the
// `active side` and to the server as the `passive side`.
//
// Design Considerations
//
// zrepl has non-standard requirements to remote procedure calls (RPC):
// whereas the coordination of replication (the planning phase) mostly
// consists of regular unary calls, the actual data transfer requires
// a high-throughput, low-overhead solution.
//
// Specifically, the requirements for data transfer is to perform
// a copy of an io.Reader over the wire, such that an io.EOF of the original
// reader corresponds to an io.EOF on the receiving side.
// If any other error occurs while reading from the original io.Reader
// on the sender, the receiver should receive the contents of that error
// in some form (usually as a trailer message)
// A common implementation technique for above data transfer is chunking,
// for example in HTTP:
// https://tools.ietf.org/html/rfc2616#section-3.6.1
//
// With regards to regular RPCs, gRPC is a popular implementation based
// on protocol buffers and thus code generation.
// gRPC also supports bi-directional streaming RPC, and it is possible
// to implement chunked transfer through the use of streams.
//
// For zrepl however, both HTTP and manually implemented chunked transfer
// using gRPC were found to have significant CPU overhead at transfer
// speeds to be expected even with hobbyist users.
//
// However, it is nice to piggyback on the code generation provided
// by protobuf / gRPC, in particular since the majority of call types
// are regular unary RPCs for which the higher overhead of gRPC is acceptable.
//
// Hence, this package attempts to combine the best of both worlds:
//
// GRPC for Coordination and Dataconn for Bulk Data Transfer
//
// This package's Client uses its transport.Connecter to maintain
// separate control and data connections to the Server.
// The control connection is used by an instance of pdu.ReplicationClient
// whose 'regular' unary RPC calls are re-exported.
// The data connection is used by an instance of dataconn.Client and
// is used for bulk data transfer, namely `Send` and `Receive`.
//
// The following ASCII diagram gives an overview of how the individual
// building blocks are glued together:
//
//                    +------------+
//                    | rpc.Client |
//                    +------------+
//                      |         |
//             +--------+         +------------+
//             |                               |
//   +---------v-----------+          +--------v------+
//   |pdu.ReplicationClient|          |dataconn.Client|
//   +---------------------+          +--------v------+
//             | label:            label:      |
//             | zrepl_control      zrepl_data |
//             +--------+         +------------+
//                      |         |
//                   +--v---------v---+
//                   |  transportmux  |
//                   +-------+--------+
//                           | uses
//                   +-------v--------+
//                   |versionhandshake|
//                   +-------+--------+
//                           | uses
//                    +------v------+
//                    |  transport  |
//                    +------+------+
//                           |
//                        NETWORK
//                           |
//                    +------+------+
//                    |  transport  |
//                    +------^------+
//                           | uses
//                   +-------+--------+
//                   |versionhandshake|
//                   +-------^--------+
//                           | uses
//                   +-------+--------+
//                   |  transportmux  |
//                   +--^--------^----+
//                      |        |
//             +--------+        --------------+       ---
//             |                               |        |
//             | label:            label:      |        |
//             | zrepl_control      zrepl_data |        |
//       +-----+----+              +-----------+---+    |
//       |netadaptor|              |dataconn.Server|    | rpc.Server
//       |     +    |              +------+--------+    |
//       |grpcclient|                     |             |
//       |identity  |                     |             |
//       +-----+----+                     |             |
//             |                          |             |
//   +---------v-----------+              |             |
//   |pdu.ReplicationServer|              |             |
//   +---------+-----------+              |             |
//             |                          |            ---
//             +----------+  +------------+
//                        |  |
//                    +---v--v-----+
//                    |  Handler   |
//                    +------------+
//           (usually endpoint.{Sender,Receiver})
//
//
package rpc

// edit trick for the ASCII art above:
// - remove the comments //
// - vim: set virtualedit+=all
// - vim: set ft=text
