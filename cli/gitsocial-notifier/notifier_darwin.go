//go:build darwin && cgo

// notifier_darwin.go - Posts through UNUserNotificationCenter and opens the URL of a clicked notification.
package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=11.0
#cgo LDFLAGS: -framework Foundation -framework AppKit -framework UserNotifications
#include <stdlib.h>
#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <UserNotifications/UserNotifications.h>

@interface GSDelegate : NSObject <UNUserNotificationCenterDelegate, NSApplicationDelegate>
@end

@implementation GSDelegate
- (void)userNotificationCenter:(UNUserNotificationCenter *)center didReceiveNotificationResponse:(UNNotificationResponse *)response withCompletionHandler:(void (^)(void))completionHandler {
	NSString *url = response.notification.request.content.userInfo[@"url"];
	NSURL *target = url.length > 0 ? [NSURL URLWithString:url] : nil;
	if (target) [[NSWorkspace sharedWorkspace] openURL:target];
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

static int gsNotify(const char *title, const char *body, const char *url) {
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
		content.userInfo = @{@"url": [NSString stringWithUTF8String:url]};
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

// notify asks for authorization on the first use and posts one notification whose click opens url.
func notify(title, body, url string) int {
	cTitle, cBody, cURL := C.CString(title), C.CString(body), C.CString(url)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cBody))
	defer C.free(unsafe.Pointer(cURL))
	return int(C.gsNotify(cTitle, cBody, cURL))
}

// handleClick runs the run loop until the response of a click arrives and opens its URL, or exits after 10 s.
func handleClick() int {
	return int(C.gsHandleClick())
}
