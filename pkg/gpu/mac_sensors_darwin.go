//go:build darwin && cgo

package gpu

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <dlfcn.h>
#include <stdint.h>
#include <string.h>

// Apple's IOReport and IOHID sensor calls are private, so they are loaded
// with dlopen: a macOS that drops them yields "unknown", not a crash.
// Callers are serialized by Monitor.mu, so the globals need no locking.

// ---- GPU power: IOReport "Energy Model" group, "GPU Energy" channel ----

typedef CFDictionaryRef (*ior_copy_channels_t)(CFStringRef, CFStringRef, uint64_t, uint64_t, uint64_t);
typedef void *(*ior_create_sub_t)(void *, CFMutableDictionaryRef, CFMutableDictionaryRef *, uint64_t, CFTypeRef);
typedef CFDictionaryRef (*ior_samples_t)(void *, CFMutableDictionaryRef, CFTypeRef);
typedef CFDictionaryRef (*ior_delta_t)(CFDictionaryRef, CFDictionaryRef, CFTypeRef);
typedef int64_t (*ior_int_t)(CFDictionaryRef, int32_t);
typedef CFStringRef (*ior_str_t)(CFDictionaryRef);

static ior_samples_t ior_samples;
static ior_delta_t ior_delta;
static ior_int_t ior_int;
static ior_str_t ior_name;
static ior_str_t ior_unit;
static void *ior_sub;
static CFMutableDictionaryRef ior_subbed;
static CFDictionaryRef ior_prev;
static double ior_prev_time;
static int ior_state; // 0 not tried, 1 ready, -1 unavailable

static int ior_init(void) {
	if (ior_state != 0) return ior_state;
	ior_state = -1;
	void *h = dlopen("/usr/lib/libIOReport.dylib", RTLD_LAZY);
	if (!h) return -1;
	ior_copy_channels_t copy_channels = (ior_copy_channels_t)dlsym(h, "IOReportCopyChannelsInGroup");
	ior_create_sub_t create_sub = (ior_create_sub_t)dlsym(h, "IOReportCreateSubscription");
	ior_samples = (ior_samples_t)dlsym(h, "IOReportCreateSamples");
	ior_delta = (ior_delta_t)dlsym(h, "IOReportCreateSamplesDelta");
	ior_int = (ior_int_t)dlsym(h, "IOReportSimpleGetIntegerValue");
	ior_name = (ior_str_t)dlsym(h, "IOReportChannelGetChannelName");
	ior_unit = (ior_str_t)dlsym(h, "IOReportChannelGetUnitLabel");
	if (!copy_channels || !create_sub || !ior_samples || !ior_delta || !ior_int || !ior_name || !ior_unit) return -1;

	CFDictionaryRef ch = copy_channels(CFSTR("Energy Model"), NULL, 0, 0, 0);
	if (!ch) return -1;
	// Kept for the life of the process: the subscription refers to it.
	CFMutableDictionaryRef mch = CFDictionaryCreateMutableCopy(kCFAllocatorDefault, CFDictionaryGetCount(ch), ch);
	CFRelease(ch);
	if (!mch) return -1;
	CFMutableDictionaryRef subbed = NULL;
	ior_sub = create_sub(NULL, mch, &subbed, 0, NULL);
	if (!ior_sub || !subbed) return -1;
	ior_subbed = subbed;
	ior_state = 1;
	return 1;
}

static double ior_unit_scale(CFStringRef unit) {
	char buf[16] = {0};
	if (!unit || !CFStringGetCString(unit, buf, sizeof buf, kCFStringEncodingUTF8)) return 0;
	char *s = buf;
	while (*s == ' ') s++;
	size_t n = strlen(s);
	while (n > 0 && s[n - 1] == ' ') s[--n] = 0;
	if (strcmp(s, "mJ") == 0) return 1e-3;
	if (strcmp(s, "uJ") == 0) return 1e-6;
	if (strcmp(s, "nJ") == 0) return 1e-9;
	return 0;
}

// Average GPU watts since the previous call; -1 when unknown (first call,
// Intel Mac, or IOReport missing).
static double mac_gpu_watts(void) {
	if (ior_init() != 1) return -1;
	CFDictionaryRef cur = ior_samples(ior_sub, ior_subbed, NULL);
	if (!cur) return -1;
	double now = CFAbsoluteTimeGetCurrent();
	double watts = -1;
	if (ior_prev) {
		double dt = now - ior_prev_time;
		CFDictionaryRef delta = ior_delta(ior_prev, cur, NULL);
		if (delta) {
			CFArrayRef items = (CFArrayRef)CFDictionaryGetValue(delta, CFSTR("IOReportChannels"));
			if (dt > 0 && items && CFGetTypeID(items) == CFArrayGetTypeID()) {
				double joules = 0;
				int found = 0;
				for (CFIndex i = 0; i < CFArrayGetCount(items); i++) {
					CFDictionaryRef item = (CFDictionaryRef)CFArrayGetValueAtIndex(items, i);
					CFStringRef name = ior_name(item);
					if (!name || !CFEqual(name, CFSTR("GPU Energy"))) continue;
					double scale = ior_unit_scale(ior_unit(item));
					if (scale == 0) continue;
					joules += (double)ior_int(item, 0) * scale;
					found = 1;
				}
				if (found) watts = joules / dt;
			}
			CFRelease(delta);
		}
		CFRelease(ior_prev);
	}
	ior_prev = cur;
	ior_prev_time = now;
	return watts;
}

// ---- GPU temperature: IOHID temperature sensors ----

typedef void *(*hid_create_t)(CFAllocatorRef);
typedef int (*hid_match_t)(void *, CFDictionaryRef);
typedef CFArrayRef (*hid_services_t)(void *);
typedef CFTypeRef (*hid_prop_t)(void *, CFStringRef);
typedef void *(*hid_event_t)(void *, int64_t, int32_t, int64_t);
typedef double (*hid_float_t)(void *, int32_t);

#define HID_EVENT_TEMPERATURE 15

static void *hid_client;
static hid_services_t hid_services;
static hid_prop_t hid_prop;
static hid_event_t hid_event;
static hid_float_t hid_float;
static int hid_state;

static int hid_init(void) {
	if (hid_state != 0) return hid_state;
	hid_state = -1;
	void *h = dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", RTLD_LAZY);
	if (!h) return -1;
	hid_create_t create = (hid_create_t)dlsym(h, "IOHIDEventSystemClientCreate");
	hid_match_t match = (hid_match_t)dlsym(h, "IOHIDEventSystemClientSetMatching");
	hid_services = (hid_services_t)dlsym(h, "IOHIDEventSystemClientCopyServices");
	hid_prop = (hid_prop_t)dlsym(h, "IOHIDServiceClientCopyProperty");
	hid_event = (hid_event_t)dlsym(h, "IOHIDServiceClientCopyEvent");
	hid_float = (hid_float_t)dlsym(h, "IOHIDEventGetFloatValue");
	if (!create || !match || !hid_services || !hid_prop || !hid_event || !hid_float) return -1;

	hid_client = create(kCFAllocatorDefault);
	if (!hid_client) return -1;
	int page = 0xff00, usage = 5; // Apple vendor page, temperature sensor
	CFNumberRef vals[2] = {
		CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &page),
		CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &usage),
	};
	const void *keys[2] = {CFSTR("PrimaryUsagePage"), CFSTR("PrimaryUsage")};
	CFDictionaryRef m = CFDictionaryCreate(kCFAllocatorDefault, keys, (const void **)vals, 2,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFRelease(vals[0]);
	CFRelease(vals[1]);
	if (!m) return -1;
	match(hid_client, m);
	CFRelease(m);
	hid_state = 1;
	return 1;
}

// Hottest GPU sensor in Celsius. Falls back to the hottest SoC die sensor
// ("tdie"), since the GPU shares the die. -1 when nothing is readable.
static double mac_gpu_temp(void) {
	if (hid_init() != 1) return -1;
	CFArrayRef svcs = hid_services(hid_client);
	if (!svcs) return -1;
	double gpu = -1, die = -1;
	for (CFIndex i = 0; i < CFArrayGetCount(svcs); i++) {
		void *svc = (void *)CFArrayGetValueAtIndex(svcs, i);
		char name[128] = {0};
		CFTypeRef p = hid_prop(svc, CFSTR("Product"));
		if (p) {
			if (CFGetTypeID(p) == CFStringGetTypeID())
				CFStringGetCString((CFStringRef)p, name, sizeof name, kCFStringEncodingUTF8);
			CFRelease(p);
		}
		void *ev = hid_event(svc, HID_EVENT_TEMPERATURE, 0, 0);
		if (!ev) continue;
		double t = hid_float(ev, HID_EVENT_TEMPERATURE << 16);
		CFRelease(ev);
		if (t <= 0 || t > 150) continue;
		if (strstr(name, "GPU")) {
			if (t > gpu) gpu = t;
		} else if (strstr(name, "tdie")) {
			if (t > die) die = t;
		}
	}
	CFRelease(svcs);
	return gpu > 0 ? gpu : die;
}
*/
import "C"

// macSensors returns GPU power (average watts since the last call) and
// temperature (Celsius). Zero means unknown.
func macSensors() (powerW, tempC float64) {
	powerW = float64(C.mac_gpu_watts())
	tempC = float64(C.mac_gpu_temp())
	if powerW < 0 {
		powerW = 0
	}
	if tempC < 0 {
		tempC = 0
	}
	return powerW, tempC
}
