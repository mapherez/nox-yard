// Package trust owns Yard's scoped Backend association and recovery journal.
package trust

const ProductID = "nox-yard"
const ContractVersion = "1.0"

type Config struct{ SocketDir, BackendURL string }
