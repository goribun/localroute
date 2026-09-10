// Native menu regression test. Run from the repository root:
// clang -fblocks -framework Cocoa tests/desktop_menu_darwin.m -o /tmp/localroute-menu-test
// /tmp/localroute-menu-test
#import <Cocoa/Cocoa.h>
#import "../desktop_darwin.m"
#include <assert.h>

static int calls;
void localrouteDesktopAction(int action) {
 assert(action == 1);
 calls++;
}

int main(void) {
 @autoreleasepool {
  LocalRouteDesktop *owner = [LocalRouteDesktop new];
  NSMenu *menu = [NSMenu new];
  menu.autoenablesItems = NO;
  owner.status = [menu addItemWithTitle:@"代理已停止" action:nil keyEquivalent:@""];
  owner.toggle = [menu addItemWithTitle:@"启动代理" action:@selector(toggle:) keyEquivalent:@""];
  owner.toggle.target = owner;
  assert(owner.toggle.enabled);
  assert([owner respondsToSelector:owner.toggle.action]);
  [owner performSelector:owner.toggle.action withObject:owner.toggle];
  assert(calls == 1);
  assert(owner.busy && !owner.toggle.enabled);
  assert([owner.toggle.title hasPrefix:@"正在启动"]);
  [owner performSelector:owner.toggle.action withObject:owner.toggle];
  assert(calls == 1);
  owner.busy = NO;
  owner.running = YES;
  [owner performSelector:owner.toggle.action withObject:owner.toggle];
  assert(calls == 2);
  assert([owner.toggle.title hasPrefix:@"正在停止"]);
  puts("Native menu callbacks, immediate feedback, and duplicate-click protection passed.");
 }
}
