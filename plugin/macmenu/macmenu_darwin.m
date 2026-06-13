// Cocoa backend for package macmenu. Builds NSMenu/NSMenuItem objects and routes
// item clicks back into Go via goMenuAction (declared in _cgo_export.h).
//
// Menus are addressed by an integer handle (an index into gMenus) and items by
// their Go-assigned tag (a key into gItems) so no Objective-C pointers cross the
// cgo boundary. Every created object is retained by these containers for the
// life of the process — menus are built once at startup.

#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

@interface SplitiMenuTarget : NSObject
- (void)onItem:(id)sender;
@end

@implementation SplitiMenuTarget
- (void)onItem:(id)sender {
	goMenuAction((int)[(NSMenuItem *)sender tag]);
}
@end

static NSMutableArray<NSMenu *> *gMenus = nil;
static NSMutableDictionary<NSNumber *, NSMenuItem *> *gItems = nil;
static SplitiMenuTarget *gTarget = nil;
// Where the next top-level menu is inserted: index 1 keeps the application menu
// (index 0) first, and successive menus follow in call order.
static NSInteger gInsertAt = 1;

static void ensureInit(void) {
	if (gMenus == nil) {
		gMenus = [[NSMutableArray alloc] init];
	}
	if (gItems == nil) {
		gItems = [[NSMutableDictionary alloc] init];
	}
	if (gTarget == nil) {
		gTarget = [[SplitiMenuTarget alloc] init];
	}
}

int macmenuNewMenu(const char *title) {
	ensureInit();
	NSMenu *m = [[NSMenu alloc] initWithTitle:[NSString stringWithUTF8String:title]];
	// Honour our explicit enabled flags rather than AppKit's responder-chain
	// auto-validation (our targets aren't in the chain).
	[m setAutoenablesItems:NO];
	[gMenus addObject:m];
	return (int)([gMenus count] - 1);
}

void macmenuAddTop(int menu, const char *title) {
	ensureInit();
	NSMenu *sub = [gMenus objectAtIndex:menu];
	NSString *t = [NSString stringWithUTF8String:title];
	[sub setTitle:t];
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:t action:nil keyEquivalent:@""];
	[item setSubmenu:sub];

	NSMenu *main = [NSApp mainMenu];
	if (main == nil) {
		main = [[NSMenu alloc] init];
		[NSApp setMainMenu:main];
	}
	NSInteger pos = gInsertAt;
	if (pos > [main numberOfItems]) {
		pos = [main numberOfItems];
	}
	[main insertItem:item atIndex:pos];
	gInsertAt = pos + 1;
}

void macmenuAddSubmenu(int parent, int child, const char *title) {
	ensureInit();
	NSMenu *p = [gMenus objectAtIndex:parent];
	NSMenu *c = [gMenus objectAtIndex:child];
	NSString *t = [NSString stringWithUTF8String:title];
	[c setTitle:t];
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:t action:nil keyEquivalent:@""];
	[item setSubmenu:c];
	[p addItem:item];
}

void macmenuAddItem(int menu, int tag, const char *title) {
	ensureInit();
	NSMenu *m = [gMenus objectAtIndex:menu];
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:[NSString stringWithUTF8String:title]
	                                              action:@selector(onItem:)
	                                       keyEquivalent:@""];
	[item setTarget:gTarget];
	[item setTag:tag];
	[item setEnabled:YES];
	[m addItem:item];
	[gItems setObject:item forKey:@(tag)];
}

void macmenuAddSeparator(int menu) {
	ensureInit();
	NSMenu *m = [gMenus objectAtIndex:menu];
	[m addItem:[NSMenuItem separatorItem]];
}

void macmenuSetKey(int tag, const char *key, unsigned long mods) {
	NSMenuItem *it = [gItems objectForKey:@(tag)];
	if (it == nil) {
		return;
	}
	[it setKeyEquivalent:[NSString stringWithUTF8String:key]];
	[it setKeyEquivalentModifierMask:(NSEventModifierFlags)mods];
}

void macmenuSetEnabled(int tag, int enabled) {
	NSMenuItem *it = [gItems objectForKey:@(tag)];
	[it setEnabled:(enabled ? YES : NO)];
}

void macmenuSetChecked(int tag, int checked) {
	NSMenuItem *it = [gItems objectForKey:@(tag)];
	[it setState:(checked ? NSControlStateValueOn : NSControlStateValueOff)];
}

void macmenuSetTitle(int tag, const char *title) {
	NSMenuItem *it = [gItems objectForKey:@(tag)];
	[it setTitle:[NSString stringWithUTF8String:title]];
}
