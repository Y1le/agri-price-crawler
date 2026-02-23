package code

//go:generate codegen -type=int

// iam-apiserver: user errors.
const (
	// ErrUserNotFound - 404: User not found.
	ErrUserNotFound int = iota + 110001

	// ErrUserAlreadyExist - 400: User already exist.
	ErrUserAlreadyExist
	ErrUserNotExist
)

// iam-apiserver: secret errors.
const (
	// ErrEncrypt - 400: Secret reach the max count.
	ErrReachMaxCount int = iota + 110101

	//  ErrSecretNotFound - 404: Secret not found.
	ErrSecretNotFound
)

// iam-apiserver: policy errors.
const (
	// ErrPolicyNotFound - 404: Policy not found.
	ErrPolicyNotFound int = iota + 110201
)

const (
	// ErrSubscribeNotFound - 404: Subscribe not found.
	ErrSubscribeNotFound int = iota + 110301

	// ErrSubscribeAlreadyExist - 400: Subscribe already exist.
	ErrSubscribeAlreadyExist
)

const (
	// ErrPriceNotFound - 404: Price not found.
	ErrPriceNotFound int = iota + 110401
)
