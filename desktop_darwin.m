//go:build darwin

#import <Cocoa/Cocoa.h>

extern void localrouteDesktopAction(int action);

@interface LocalRouteDesktop : NSObject
@property(nonatomic, retain) NSStatusItem *item;
@property(nonatomic, retain) NSMenuItem *status;
@property(nonatomic, retain) NSMenuItem *toggle;
@property(nonatomic, retain) id activity;
@property(nonatomic) BOOL busy;
@property(nonatomic) BOOL running;
@end

@implementation LocalRouteDesktop
- (void)show:(id)sender { localrouteDesktopAction(0); }
- (void)toggle:(id)sender {
 if (self.busy) return;
 self.busy = YES;
 self.toggle.enabled = NO;
 self.toggle.title = self.running ? @"正在停止…" : @"正在启动…（如有授权提示，请完成授权）";
 self.status.title = self.running ? @"正在停止代理…" : @"正在启动代理…";
 self.item.button.title = @"LR …";
 // A status-item click does not activate the app. Activate it before requesting
 // privileged-port authorization so the system prompt is not left behind.
 [NSApp activateIgnoringOtherApps:YES];
 localrouteDesktopAction(1);
}
- (void)quit:(id)sender { localrouteDesktopAction(2); }
- (void)wake:(NSNotification *)notification { localrouteDesktopAction(3); }
@end

static LocalRouteDesktop *desktop;

void localrouteDesktopStart(void) {
 dispatch_async(dispatch_get_main_queue(), ^{
  if (desktop) return;
  desktop = [LocalRouteDesktop new];
  desktop.item = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
  desktop.item.button.title = @"LR ○";
  desktop.item.button.toolTip = @"LocalRoute · 代理已停止";
  NSMenu *menu = [[[NSMenu alloc] init] autorelease];
  menu.autoenablesItems = NO;
  desktop.status = [menu addItemWithTitle:@"代理已停止" action:nil keyEquivalent:@""];
  desktop.status.enabled = NO;
  [menu addItem:[NSMenuItem separatorItem]];
  NSMenuItem *show = [menu addItemWithTitle:@"打开 LocalRoute" action:@selector(show:) keyEquivalent:@""];
  show.target = desktop;
  desktop.toggle = [menu addItemWithTitle:@"启动代理" action:@selector(toggle:) keyEquivalent:@""];
  desktop.toggle.target = desktop;
  [menu addItem:[NSMenuItem separatorItem]];
  NSMenuItem *quit = [menu addItemWithTitle:@"退出 LocalRoute" action:@selector(quit:) keyEquivalent:@""];
  quit.target = desktop;
  desktop.item.menu = menu;
  [[[NSWorkspace sharedWorkspace] notificationCenter] addObserver:desktop selector:@selector(wake:) name:NSWorkspaceDidWakeNotification object:nil];
 });
}

void localrouteDesktopUpdate(int running, const char *status) {
 NSString *label = [[NSString alloc] initWithUTF8String:status];
 dispatch_async(dispatch_get_main_queue(), ^{
  // Keep background proxy work responsive without preventing system sleep.
  if (desktop && running && !desktop.activity) {
   desktop.activity = [[NSProcessInfo processInfo] beginActivityWithOptions:NSActivityUserInitiatedAllowingIdleSystemSleep reason:@"LocalRoute proxy is running"];
  } else if (!running && desktop.activity) {
   [[NSProcessInfo processInfo] endActivity:desktop.activity];
   desktop.activity = nil;
  }
  desktop.running = running;
  if (!desktop.busy) desktop.item.button.title = running ? @"LR ●" : @"LR ○";
  desktop.item.button.toolTip = [@"LocalRoute · " stringByAppendingString:label];
  if (!desktop.busy) desktop.status.title = label;
  if (!desktop.busy) desktop.toggle.title = running ? @"停止代理" : @"启动代理";
  [label release];
 });
}

void localrouteDesktopStop(void) {
 dispatch_async(dispatch_get_main_queue(), ^{
  if (!desktop) return;
  [[[NSWorkspace sharedWorkspace] notificationCenter] removeObserver:desktop];
  if (desktop.activity) {
   [[NSProcessInfo processInfo] endActivity:desktop.activity];
   desktop.activity = nil;
  }
  [[NSStatusBar systemStatusBar] removeStatusItem:desktop.item];
  desktop.item = nil;
  desktop.status = nil;
  desktop.toggle = nil;
  [desktop release];
  desktop = nil;
 });
}

void localrouteDesktopActionFinished(void) {
 dispatch_async(dispatch_get_main_queue(), ^{
  desktop.busy = NO;
  desktop.toggle.enabled = YES;
 });
}
