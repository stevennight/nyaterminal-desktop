//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>

int nya_verify_user_presence(const char *reason, char **error_message) {
    @autoreleasepool {
        LAContext *context = [[LAContext alloc] init];
        NSError *availabilityError = nil;
        LAPolicy policy = LAPolicyDeviceOwnerAuthentication;
        if (![context canEvaluatePolicy:policy error:&availabilityError]) {
            if (error_message != NULL) {
                const char *message = availabilityError.localizedDescription.UTF8String;
                *error_message = strdup(message != NULL ? message : "macOS authentication is unavailable");
            }
            return 0;
        }
        dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);
        __block BOOL verified = NO;
        __block NSString *failure = nil;
        NSString *localizedReason = [NSString stringWithUTF8String:reason];
        [context evaluatePolicy:policy localizedReason:localizedReason reply:^(BOOL success, NSError *error) {
            verified = success;
            if (!success) {
                failure = error.localizedDescription;
            }
            dispatch_semaphore_signal(semaphore);
        }];
        dispatch_semaphore_wait(semaphore, DISPATCH_TIME_FOREVER);
        if (!verified && error_message != NULL) {
            const char *message = failure.UTF8String;
            *error_message = strdup(message != NULL ? message : "macOS authentication failed");
        }
        return verified ? 1 : 0;
    }
}
