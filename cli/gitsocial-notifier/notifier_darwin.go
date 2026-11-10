//go:build darwin && cgo

// notifier_darwin.go - Posts through UNUserNotificationCenter and handles the click of a notification.
package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=11.0
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework UserNotifications
#include <stdlib.h>
#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <UserNotifications/UserNotifications.h>

extern int gsClick(char *url, char *socket, char *ref);

@interface GSDelegate : NSObject <UNUserNotificationCenterDelegate, NSApplicationDelegate>
@end

@implementation GSDelegate
- (void)userNotificationCenter:(UNUserNotificationCenter *)center didReceiveNotificationResponse:(UNNotificationResponse *)response withCompletionHandler:(void (^)(void))completionHandler {
	NSDictionary *info = response.notification.request.content.userInfo;
	NSString *url = info[@"url"] ?: @"";
	NSString *socket = info[@"socket"] ?: @"";
	NSString *ref = info[@"ref"] ?: @"";
	NSURL *target = url.length > 0 ? [NSURL URLWithString:url] : nil;
	if (gsClick((char *)url.UTF8String, (char *)socket.UTF8String, (char *)ref.UTF8String) && target) [[NSWorkspace sharedWorkspace] openURL:target];
	completionHandler();
	exit(0);
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center willPresentNotification:(UNNotification *)notification withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completionHandler {
	completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList);
}
@end

static GSDelegate *gsDelegate;

static void gsSetup(void) {
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
	gsDelegate = [GSDelegate new];
	[NSApp setDelegate:gsDelegate];
	[UNUserNotificationCenter currentNotificationCenter].delegate = gsDelegate;
}

static int gsNotify(const char *title, const char *body, const char *url, const char *socket, const char *ref) {
	@autoreleasepool {
		if ([NSBundle mainBundle].bundleIdentifier == nil) {
			fprintf(stderr, "gitsocial-notifier: no bundle identifier, start it from GitSocial.app\n");
			return 1;
		}
		gsSetup();
		UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
		UNMutableNotificationContent *content = [UNMutableNotificationContent new];
		content.title = [NSString stringWithUTF8String:title];
		content.body = [NSString stringWithUTF8String:body];
		content.userInfo = @{@"url": [NSString stringWithUTF8String:url], @"socket": [NSString stringWithUTF8String:socket], @"ref": [NSString stringWithUTF8String:ref]};
		__block int rc = 0;
		__block BOOL done = NO;
		[center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionSound) completionHandler:^(BOOL granted, NSError *error) {
			if (!granted) {
				fprintf(stderr, "gitsocial-notifier: notifications are not allowed: %s\n", error ? error.description.UTF8String : "denied");
				rc = 1;
				done = YES;
				return;
			}
			UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:[[NSUUID UUID] UUIDString] content:content trigger:nil];
			[center addNotificationRequest:request withCompletionHandler:^(NSError *addError) {
				if (addError) {
					fprintf(stderr, "gitsocial-notifier: post: %s\n", addError.description.UTF8String);
					rc = 1;
				}
				done = YES;
			}];
		}];
		NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:30];
		while (!done && [deadline timeIntervalSinceNow] > 0) {
			[[NSRunLoop mainRunLoop] runMode:NSDefaultRunLoopMode beforeDate:[NSDate dateWithTimeIntervalSinceNow:0.1]];
		}
		if (!done) {
			fprintf(stderr, "gitsocial-notifier: no answer from the notification center\n");
			return 1;
		}
		return rc;
	}
}

static int gsHandleClick(void) {
	@autoreleasepool {
		gsSetup();
		[NSTimer scheduledTimerWithTimeInterval:10 repeats:NO block:^(NSTimer *timer) {
			exit(0);
		}];
		[NSApp run];
		return 0;
	}
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// init keeps main on the main thread, which AppKit needs.
func init() {
	runtime.LockOSThread()
}

// notify asks for authorization on the first use and posts one notification whose click goes to socket, else opens url.
func notify(title, body, url, socket, ref string) int {
	cTitle, cBody, cURL, cSocket, cRef := C.CString(title), C.CString(body), C.CString(url), C.CString(socket), C.CString(ref)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cBody))
	defer C.free(unsafe.Pointer(cURL))
	defer C.free(unsafe.Pointer(cSocket))
	defer C.free(unsafe.Pointer(cRef))
	return int(C.gsNotify(cTitle, cBody, cURL, cSocket, cRef))
}

// handleClick runs the run loop until the response of a click arrives and handles it, or exits after 10 s.
func handleClick() int {
	return int(C.gsHandleClick())
}
