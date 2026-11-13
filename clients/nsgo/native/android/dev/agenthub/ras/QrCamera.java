package dev.agenthub.ras;

import android.Manifest;
import android.content.pm.PackageManager;
import android.graphics.ImageFormat;
import android.graphics.Matrix;
import android.graphics.SurfaceTexture;
import android.hardware.camera2.*;
import android.media.Image;
import android.media.ImageReader;
import android.os.Handler;
import android.os.HandlerThread;
import android.util.Size;
import android.view.Surface;
import android.view.TextureView;
import android.view.View;
import android.widget.FrameLayout;
import dev.oneo.nsgo.NsgoComponent;
import com.google.zxing.*;
import com.google.zxing.common.HybridBinarizer;
import java.nio.ByteBuffer;
import java.util.*;
import java.util.concurrent.atomic.AtomicBoolean;

/** Camera2 live preview + offline QR-only ZXing decoder. No photo is saved. */
final class QrCamera implements NsgoComponent, TextureView.SurfaceTextureListener {
    static final int PERMISSION = 2633;
    final RasActivity activity;
    final Events events;
    final FrameLayout root;
    final TextureView texture;
    final Handler main = new Handler(android.os.Looper.getMainLooper());
    final HandlerThread worker = new HandlerThread("ras-qr-camera");
    final Handler cameraHandler;
    final AtomicBoolean delivered = new AtomicBoolean();
    final MultiFormatReader decoder = new MultiFormatReader();
    CameraDevice camera;
    CameraCaptureSession session;
    ImageReader reader;
    Surface preview;
    boolean destroyed, opening, asked, active = true;
    int generation, sensorRotation;
    Size previewSize;
    long lastFrame;

    QrCamera(RasActivity activity, Events events) {
        this.activity = activity; this.events = events;
        root = new FrameLayout(activity);
        root.setBackgroundColor(0xff000000);
        texture = new TextureView(activity);
        root.addView(texture, new FrameLayout.LayoutParams(-1,-1));
        texture.setSurfaceTextureListener(this);
        decoder.setHints(Collections.singletonMap(DecodeHintType.POSSIBLE_FORMATS, Collections.singletonList(BarcodeFormat.QR_CODE)));
        worker.start(); cameraHandler = new Handler(worker.getLooper());
        activity.scanners.add(this);
        // Ask only when the user opens this component, never at app startup.
        main.post(this::resume);
    }
    public View view() { return root; }
    public void setProp(String name, Object value) {
        if (name.equals("active")) {
            active = !Boolean.FALSE.equals(value);
            if (active) resume(); else pause();
        }
    }
    void fail(String message) {
        main.post(() -> { if (!destroyed && active) events.emit("error", message); });
    }
    void permissionResult() {
        if (activity.checkSelfPermission(Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) resume();
        else fail("相机权限未开启，请在系统设置中允许 RAS 使用相机；也可从相册识别或手动输入设备 ID。");
    }
    void resume() {
        if (destroyed || !active || !activity.resumed || delivered.get()) return;
        if (activity.checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
            if (!asked) { asked = true; activity.requestPermissions(new String[]{Manifest.permission.CAMERA}, PERMISSION); }
            return;
        }
        if (!texture.isAvailable() || camera != null || opening) return;
        opening = true;
        final int epoch = ++generation;
        try {
            CameraManager manager = (CameraManager)activity.getSystemService(android.content.Context.CAMERA_SERVICE);
            String id = null;
            for (String candidate : manager.getCameraIdList()) {
                CameraCharacteristics c = manager.getCameraCharacteristics(candidate);
                if (Integer.valueOf(CameraCharacteristics.LENS_FACING_BACK).equals(c.get(CameraCharacteristics.LENS_FACING))) { id=candidate; break; }
            }
            if (id == null) throw new IllegalStateException("没有可用的后置摄像头");
            CameraCharacteristics c = manager.getCameraCharacteristics(id);
            sensorRotation = c.get(CameraCharacteristics.SENSOR_ORIENTATION);
            android.hardware.camera2.params.StreamConfigurationMap map = c.get(CameraCharacteristics.SCALER_STREAM_CONFIGURATION_MAP);
            List<Size> sizes = Arrays.asList(map.getOutputSizes(SurfaceTexture.class));
            // Pick a modest preview size shared with the YUV reader.
            List<Size> yuv = Arrays.asList(map.getOutputSizes(ImageFormat.YUV_420_888));
            previewSize = sizes.stream().filter(yuv::contains).filter(s -> s.getWidth()<=1280 && s.getHeight()<=960)
                .max(Comparator.comparingInt(s -> s.getWidth()*s.getHeight())).orElseThrow(() -> new IllegalStateException("摄像头没有支持的扫码预览尺寸"));
            reader = ImageReader.newInstance(previewSize.getWidth(),previewSize.getHeight(),ImageFormat.YUV_420_888,2);
            reader.setOnImageAvailableListener(this::decode, cameraHandler);
            manager.openCamera(id, new CameraDevice.StateCallback() {
                public void onOpened(CameraDevice device) { main.post(() -> {
                    if (destroyed || !active || !activity.resumed || epoch!=generation) { device.close(); return; }
                    camera=device; opening=false; configure(epoch); }); }
                public void onDisconnected(CameraDevice device) { device.close(); main.post(() -> { if (epoch==generation) { pause(); fail("相机已断开，请关闭扫码后重试。"); } }); }
                public void onError(CameraDevice device,int error) { device.close(); main.post(() -> { if (epoch==generation) { pause(); fail("相机暂不可用，请关闭其它使用相机的应用后重试。"); } }); }
            },cameraHandler);
        } catch (Exception e) { pause(); fail("无法打开相机："+e.getMessage()); }
    }
    void configure(int epoch) {
        try {
            SurfaceTexture surface = texture.getSurfaceTexture();
            if(surface==null) { pause(); return; }
            surface.setDefaultBufferSize(previewSize.getWidth(),previewSize.getHeight());
            transform();
            preview = new Surface(surface);
            final CaptureRequest.Builder request = camera.createCaptureRequest(CameraDevice.TEMPLATE_PREVIEW);
            request.addTarget(preview); request.addTarget(reader.getSurface());
            request.set(CaptureRequest.CONTROL_AF_MODE,CaptureRequest.CONTROL_AF_MODE_CONTINUOUS_PICTURE);
            camera.createCaptureSession(Arrays.asList(preview,reader.getSurface()),new CameraCaptureSession.StateCallback(){
                public void onConfigured(CameraCaptureSession configured){ main.post(() -> {
                    if (destroyed || epoch!=generation || camera==null) { configured.close(); return; }
                    session=configured;
                    try { session.setRepeatingRequest(request.build(),null,cameraHandler); }
                    catch(Exception e){ pause(); fail("无法启动扫码预览。"); }
                }); }
                public void onConfigureFailed(CameraCaptureSession configured){ configured.close(); main.post(() -> { if(epoch==generation) { pause(); fail("相机预览配置失败。"); } }); }
            },cameraHandler);
        } catch(Exception e) { pause(); fail("无法配置相机："+e.getMessage()); }
    }
    void transform() {
        if (previewSize==null || texture.getWidth()==0 || texture.getHeight()==0) return;
        int display = activity.getWindowManager().getDefaultDisplay().getRotation()*90;
        int rotation = (sensorRotation-display+360)%360;
        float w=texture.getWidth(),h=texture.getHeight();
        boolean swapped=rotation==90||rotation==270;
        float pw=swapped?previewSize.getHeight():previewSize.getWidth();
        float ph=swapped?previewSize.getWidth():previewSize.getHeight();
        float scale=Math.max(w/pw,h/ph);
        Matrix m=new Matrix();
        // TextureView initially stretches the buffer to its bounds. Undo that,
        // rotate into device orientation, then center-crop without distortion.
        m.setScale(previewSize.getWidth()/w,previewSize.getHeight()/h,w/2,h/2);
        m.postRotate(rotation,w/2,h/2); m.postScale(scale,scale,w/2,h/2);
        texture.setTransform(m);
    }
    void decode(ImageReader source) {
        try (Image image=source.acquireLatestImage()) {
            if(image==null || delivered.get()) return;
            long now=android.os.SystemClock.elapsedRealtime();
            if(now-lastFrame<180) return; lastFrame=now;
            Image.Plane y=image.getPlanes()[0]; ByteBuffer b=y.getBuffer();
            int w=image.getWidth(),h=image.getHeight(),stride=y.getRowStride(),pixel=y.getPixelStride(),offset=b.position();
            byte[] luma=new byte[w*h];
            for(int row=0;row<h;row++) for(int col=0;col<w;col++) luma[row*w+col]=b.get(offset+row*stride+col*pixel);
            LuminanceSource lum=new PlanarYUVLuminanceSource(luma,w,h,0,0,w,h,false);
            Result result;
            try { result=decoder.decodeWithState(new BinaryBitmap(new HybridBinarizer(lum))); }
            catch(NotFoundException e) { decoder.reset(); result=decoder.decodeWithState(new BinaryBitmap(new HybridBinarizer(lum.invert()))); }
            if(result.getText()!=null && delivered.compareAndSet(false,true)) {
                String code=result.getText(); main.post(() -> { if(!destroyed && active) { pause(); events.emit("code",code); } });
            }
        } catch (ReaderException ignored) { } catch (IllegalStateException ignored) { }
        finally { decoder.reset(); }
    }
    void pause() {
        ++generation; opening=false;
        if(session!=null){session.close();session=null;}
        if(camera!=null){camera.close();camera=null;}
        if(reader!=null){reader.close();reader=null;}
        if(preview!=null){preview.release();preview=null;}
    }
    public void command(String name,Object args){
        if(name.equals("retry")){ delivered.set(false); resume(); }
    }
    public void destroy(){ destroyed=true; pause(); activity.scanners.remove(this); worker.quitSafely(); }
    public void onSurfaceTextureAvailable(SurfaceTexture t,int w,int h){ resume(); }
    public void onSurfaceTextureSizeChanged(SurfaceTexture t,int w,int h){ transform(); }
    public boolean onSurfaceTextureDestroyed(SurfaceTexture t){ pause(); return true; }
    public void onSurfaceTextureUpdated(SurfaceTexture t){}
}
