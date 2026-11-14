//go:build ios

#import <UIKit/UIKit.h>
#import <AVFoundation/AVFoundation.h>
#include "camera_ios.h"
extern void rasQrEvent(uint64_t token, char *name, char *value);

@interface RasQrCamera : UIView <AVCaptureMetadataOutputObjectsDelegate> {
    uint64_t _token;
    AVCaptureSession *_session;
    AVCaptureVideoPreviewLayer *_preview;
    dispatch_queue_t _queue;
    BOOL _active, _closed, _delivered, _configured, _configuring;
}
- (id)initWithToken:(uint64_t)token;
- (void)setActive:(BOOL)active;
- (void)retry;
- (void)shutdown;
@end

@implementation RasQrCamera
- (id)initWithToken:(uint64_t)token {
    if ((self=[super initWithFrame:CGRectZero])) {
        _token=token; _active=YES;
        self.backgroundColor=[UIColor blackColor];
        _session=[[AVCaptureSession alloc] init];
        _queue=dispatch_queue_create("ras.qr-camera",DISPATCH_QUEUE_SERIAL);
        _preview=[[AVCaptureVideoPreviewLayer alloc] initWithSession:_session];
        _preview.videoGravity=AVLayerVideoGravityResizeAspectFill;
        [self.layer addSublayer:_preview];
        [[NSNotificationCenter defaultCenter] addObserver:self selector:@selector(background:) name:UIApplicationWillResignActiveNotification object:nil];
        [[NSNotificationCenter defaultCenter] addObserver:self selector:@selector(foreground:) name:UIApplicationDidBecomeActiveNotification object:nil];
        [[NSNotificationCenter defaultCenter] addObserver:self selector:@selector(sessionError:) name:AVCaptureSessionRuntimeErrorNotification object:_session];
        // Return the view to NSGo before requesting permission or emitting errors.
        dispatch_async(dispatch_get_main_queue(), ^{ [self start]; });
    }
    return self;
}
- (void)emit:(NSString *)event text:(NSString *)text {
    if (!_closed) rasQrEvent(_token,(char *)event.UTF8String,(char *)text.UTF8String);
}
- (BOOL)wanted { @synchronized(self) { return !_closed && _active && !_delivered; } }
- (void)start {
    if (![self wanted] || [UIApplication sharedApplication].applicationState!=UIApplicationStateActive) return;
    AVAuthorizationStatus status=[AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo];
    if (status==AVAuthorizationStatusNotDetermined) {
        [AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo completionHandler:^(BOOL granted){
            dispatch_async(dispatch_get_main_queue(), ^{ if(granted) [self start]; else [self emit:@"error" text:@"相机权限未开启，请在系统设置中允许 ReignHub 使用相机；也可从相册识别或手动输入设备 ID。"]; });
        }]; return;
    }
    if (status!=AVAuthorizationStatusAuthorized) { [self emit:@"error" text:@"相机权限未开启，请在系统设置中允许 ReignHub 使用相机。"]; return; }
    if (_configuring) return;
    _configuring=YES;
    dispatch_async(_queue, ^{
        NSString *failure=nil;
        if ([self wanted] && !self->_configured) {
            AVCaptureDevice *device=[AVCaptureDevice defaultDeviceWithDeviceType:AVCaptureDeviceTypeBuiltInWideAngleCamera mediaType:AVMediaTypeVideo position:AVCaptureDevicePositionBack];
            NSError *error=nil;
            AVCaptureDeviceInput *input=device?[AVCaptureDeviceInput deviceInputWithDevice:device error:&error]:nil;
            AVCaptureMetadataOutput *output=[[[AVCaptureMetadataOutput alloc] init] autorelease];
            [self->_session beginConfiguration];
            if([self->_session canSetSessionPreset:AVCaptureSessionPreset1280x720]) self->_session.sessionPreset=AVCaptureSessionPreset1280x720;
            if(input && [self->_session canAddInput:input]) {
                [self->_session addInput:input];
                if([self->_session canAddOutput:output]) {
                    [self->_session addOutput:output];
                    [output setMetadataObjectsDelegate:self queue:dispatch_get_main_queue()];
                    if([output.availableMetadataObjectTypes containsObject:AVMetadataObjectTypeQRCode]) {
                        output.metadataObjectTypes=@[AVMetadataObjectTypeQRCode]; self->_configured=YES;
                    }
                }
            }
            if(!self->_configured) {
                failure=error.localizedDescription?:@"没有可用的摄像头，模拟器可使用相册识别或手动输入设备 ID。";
                NSArray *inputs=[self->_session.inputs copy];
                for(AVCaptureInput *in in inputs) [self->_session removeInput:in];
                [inputs release];
                NSArray *outputs=[self->_session.outputs copy];
                for(AVCaptureOutput *out in outputs) [self->_session removeOutput:out];
                [outputs release];
            }
            [self->_session commitConfiguration];
        }
        if([self wanted] && self->_configured) [self->_session startRunning];
        dispatch_async(dispatch_get_main_queue(), ^{ self->_configuring=NO; if(failure) [self emit:@"error" text:failure]; });
    });
}
- (void)layoutSubviews {
    [super layoutSubviews]; _preview.frame=self.bounds;
    AVCaptureConnection *connection=_preview.connection;
    if(connection.isVideoOrientationSupported && self.window.windowScene) {
        UIInterfaceOrientation o=self.window.windowScene.interfaceOrientation;
        if(o!=UIInterfaceOrientationUnknown) connection.videoOrientation=(AVCaptureVideoOrientation)o;
    }
}
- (void)setActive:(BOOL)active {
    @synchronized(self) { _active=active; }
    if(active) [self start]; else dispatch_async(_queue, ^{ [self->_session stopRunning]; });
}
- (void)background:(NSNotification *)note { dispatch_async(_queue, ^{ [self->_session stopRunning]; }); }
- (void)foreground:(NSNotification *)note { [self start]; }
- (void)sessionError:(NSNotification *)note {
    dispatch_async(dispatch_get_main_queue(), ^{ if([self wanted]) [self emit:@"error" text:@"相机暂不可用，请关闭扫码后重试。"]; });
}
- (void)metadataOutput:(AVCaptureMetadataOutput *)output didOutputMetadataObjects:(NSArray *)objects fromConnection:(AVCaptureConnection *)connection {
    if(![self wanted]) return;
    for(AVMetadataObject *object in objects) {
        if([object isKindOfClass:[AVMetadataMachineReadableCodeObject class]]) {
            NSString *text=((AVMetadataMachineReadableCodeObject *)object).stringValue;
            if(text.length) { @synchronized(self) { _delivered=YES; }
                dispatch_async(_queue, ^{ [self->_session stopRunning]; });
                [self emit:@"code" text:text]; break;
            }
        }
    }
}
- (void)retry { @synchronized(self) { _delivered=NO; } [self start]; }
- (void)shutdown {
    @synchronized(self) { _closed=YES; _active=NO; }
    [[NSNotificationCenter defaultCenter] removeObserver:self];
    dispatch_async(_queue, ^{ [self->_session stopRunning]; });
}
- (void)dealloc { [_preview release]; [_session release]; dispatch_release(_queue); [super dealloc]; }
@end

uintptr_t ras_camera_create(uint64_t token) { return (uintptr_t)[[RasQrCamera alloc] initWithToken:token]; }
void ras_camera_active(uintptr_t view,int active) { [(RasQrCamera *)view setActive:active!=0]; }
void ras_camera_retry(uintptr_t view) { [(RasQrCamera *)view retry]; }
void ras_camera_close(uintptr_t view) { RasQrCamera *camera=(RasQrCamera *)view; [camera shutdown]; [camera release]; }
char *ras_device_name(void) {
    __block char *name=NULL;
    void (^read)(void)=^{ name=strdup([UIDevice currentDevice].name.UTF8String); };
    if([NSThread isMainThread]) read(); else dispatch_sync(dispatch_get_main_queue(),read);
    return name;
}
