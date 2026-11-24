#import <CoreServices/CoreServices.h>
#import <Foundation/Foundation.h>

/** 把 URL scheme 绑到指定 bundle id（避免绑到裸 Electron → 欢迎页）。 */
int main(int argc, char **argv) {
  if (argc < 3) {
    fprintf(stderr, "usage: %s <scheme> <bundle-id>\n", argv[0]);
    return 2;
  }
  @autoreleasepool {
    CFStringRef scheme = (__bridge CFStringRef)[NSString stringWithUTF8String:argv[1]];
    CFStringRef bid = (__bridge CFStringRef)[NSString stringWithUTF8String:argv[2]];
    OSStatus st = LSSetDefaultHandlerForURLScheme(scheme, bid);
    if (st != noErr) {
      fprintf(stderr, "LSSetDefaultHandlerForURLScheme failed: %d\n", (int)st);
      return 1;
    }
    return 0;
  }
}
