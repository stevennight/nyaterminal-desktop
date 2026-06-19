//go:build windows

package vault

import (
	"errors"
	"fmt"
	"time"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-winrtapi/winrt"
)

const userPresenceMethod = "Windows Hello"

type asyncResult[T any] struct {
	value T
	err   error
}

func awaitWinRT[T any](operation *winrt.IAsyncOperation[T]) (T, error) {
	var zero T
	if operation == nil {
		return zero, errors.New("Windows Hello operation could not be created")
	}
	completed := make(chan asyncResult[T], 1)
	operation.Put_Completed(func(info *winrt.IAsyncOperation[T], status winrt.AsyncStatus) com.Error {
		switch status {
		case winrt.AsyncStatus_Completed:
			completed <- asyncResult[T]{value: info.GetResults()}
		case winrt.AsyncStatus_Canceled:
			completed <- asyncResult[T]{err: errors.New("Windows Hello verification was cancelled")}
		default:
			completed <- asyncResult[T]{err: errors.New("Windows Hello verification failed")}
		}
		return com.OK
	})
	select {
	case result := <-completed:
		return result.value, result.err
	case <-time.After(2 * time.Minute):
		return zero, errors.New("Windows Hello verification timed out")
	}
}

func verifyUserPresence(message string) error {
	initialized := winrt.InitializeMt()
	defer initialized.Uninitialize()

	verifier := winrt.NewIUserConsentVerifierStatics()
	if verifier == nil {
		return errors.New("Windows Hello is unavailable")
	}
	availability, err := awaitWinRT(verifier.CheckAvailabilityAsync())
	if err != nil {
		return err
	}
	if availability != winrt.UserConsentVerifierAvailability_Available {
		return fmt.Errorf("Windows Hello is unavailable: %s", helloAvailabilityMessage(availability))
	}
	result, err := awaitWinRT(verifier.RequestVerificationAsync(message))
	if err != nil {
		return err
	}
	if result != winrt.UserConsentVerificationResult_Verified {
		return fmt.Errorf("Windows Hello did not verify the user: %s", helloResultMessage(result))
	}
	return nil
}

func helloAvailabilityMessage(value winrt.UserConsentVerifierAvailability) string {
	switch value {
	case winrt.UserConsentVerifierAvailability_DeviceNotPresent:
		return "no compatible biometric or PIN device is present"
	case winrt.UserConsentVerifierAvailability_NotConfiguredForUser:
		return "Windows Hello is not configured for this user"
	case winrt.UserConsentVerifierAvailability_DisabledByPolicy:
		return "disabled by system policy"
	case winrt.UserConsentVerifierAvailability_DeviceBusy:
		return "the verification device is busy"
	default:
		return "unknown availability state"
	}
}

func helloResultMessage(value winrt.UserConsentVerificationResult) string {
	switch value {
	case winrt.UserConsentVerificationResult_Canceled:
		return "verification was cancelled"
	case winrt.UserConsentVerificationResult_RetriesExhausted:
		return "verification retries were exhausted"
	case winrt.UserConsentVerificationResult_NotConfiguredForUser:
		return "Windows Hello is not configured for this user"
	case winrt.UserConsentVerificationResult_DisabledByPolicy:
		return "disabled by system policy"
	case winrt.UserConsentVerificationResult_DeviceNotPresent:
		return "no compatible biometric or PIN device is present"
	case winrt.UserConsentVerificationResult_DeviceBusy:
		return "the verification device is busy"
	default:
		return "verification failed"
	}
}
