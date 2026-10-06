//go:build tray

#import <AppKit/AppKit.h>
#import <objc/message.h>
#import <objc/runtime.h>

// macOS 27 hides menu item images unless an item opts in through
// preferredImageVisibility, which fyne.io/systray never sets, so the provider
// icons vanish. Wrap -[NSMenuItem setImage:] to opt every item that gets an
// image in. The property is looked up at run time so this builds against older
// SDKs and does nothing before macOS 27.

static IMP pbOrigSetImage;
static SEL pbSetVisibility;

enum { pbImageVisibilityAutomatic = 0, pbImageVisibilityVisible = 1 };

static void pbSetImage(id self, SEL _cmd, NSImage *image) {
	((void (*)(id, SEL, NSImage *))pbOrigSetImage)(self, _cmd, image);
	if ([self respondsToSelector:pbSetVisibility]) {
		NSInteger v = image ? pbImageVisibilityVisible : pbImageVisibilityAutomatic;
		((void (*)(id, SEL, NSInteger))objc_msgSend)(self, pbSetVisibility, v);
	}
}

__attribute__((constructor)) static void pbShowMenuImages(void) {
	pbSetVisibility = sel_registerName("setPreferredImageVisibility:");
	Method m = class_getInstanceMethod([NSMenuItem class], @selector(setImage:));
	pbOrigSetImage = method_setImplementation(m, (IMP)pbSetImage);
}

// pbMenuImageVisibility reports an item's visibility after setting an image,
// or -1 where the property doesn't exist. Used by tests.
long pbMenuImageVisibility(void) {
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:@"x" action:nil keyEquivalent:@""];
	item.image = [[NSImage alloc] initWithSize:NSMakeSize(16, 16)];
	SEL get = sel_registerName("preferredImageVisibility");
	if (![item respondsToSelector:get]) {
		return -1;
	}
	return (long)((NSInteger (*)(id, SEL))objc_msgSend)(item, get);
}
