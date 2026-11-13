#!/usr/bin/env python3
"""Build native extensions through NSGo's official AAR/embedding API.

No framework fork or modification of the Go module cache. Uses the existing
NSGo debug certificate, so this APK upgrades the previous NSGo installation.
"""
import argparse, hashlib, os, pathlib, shutil, subprocess, tempfile, urllib.request, zipfile

root = pathlib.Path(__file__).resolve().parent.parent
parser = argparse.ArgumentParser()
parser.add_argument('--output', default='dist')
parser.add_argument('--targets', required=True)
args = parser.parse_args()
out = pathlib.Path(args.output).resolve()
android = [t for t in args.targets.split(',') if t.startswith('android_')]
if not android:
    raise SystemExit(0)
sdk = pathlib.Path(os.environ.get('ANDROID_HOME', str(pathlib.Path.home()/'Library/Android/sdk')))
platforms = sorted(sdk.glob('platforms/android-*/android.jar'))
d8s = sorted(sdk.glob('build-tools/*/d8'))
if not platforms or not d8s or not shutil.which('javac'):
    raise SystemExit('Native QR host requires JDK 17+ and an Android SDK (ANDROID_HOME).')
cache = root/'.nsgo/native-android'
cache.mkdir(parents=True, exist_ok=True)
zxing = cache/'zxing-core-3.5.3.jar'
expected = '8d8064c1636fdaef7189dd9055c7d59950a8940a12f2293956446ec3c109fd82'
if not zxing.exists():
    with urllib.request.urlopen('https://repo.maven.apache.org/maven2/com/google/zxing/core/3.5.3/core-3.5.3.jar') as r:
        zxing.write_bytes(r.read())
if hashlib.sha256(zxing.read_bytes()).hexdigest() != expected:
    raise SystemExit('ZXing dependency checksum mismatch')
for target in android:
    with tempfile.TemporaryDirectory(prefix='ras-native-android-') as tmp:
        tmp = pathlib.Path(tmp)
        subprocess.run(['go','tool','nsgo','build','-embed','-target',target,'-output',str(tmp/'aar')],cwd=root,check=True)
        aar = next((tmp/'aar').rglob('RAS.aar'))
        with zipfile.ZipFile(aar) as z:
            (tmp/'classes.jar').write_bytes(z.read('classes.jar'))
        classes = tmp/'classes'; classes.mkdir()
        sources = list((root/'native/android').rglob('*.java'))
        jar = str(platforms[-1])
        subprocess.run(['javac','-nowarn','--release','17','-classpath',os.pathsep.join([jar,str(tmp/'classes.jar'),str(zxing)]),'-d',str(classes),*map(str,sources)],check=True)
        subprocess.run([str(d8s[-1]),'--lib',jar,'--min-api','28','--output',str(tmp),str(tmp/'classes.jar'),str(zxing),*map(str,classes.rglob('*.class'))],check=True)
        matches = list(out.glob(f'RAS-*-{target}/RAS.apk'))
        if len(matches) != 1:
            raise SystemExit(f'Expected one {target} APK in {out}, found {len(matches)}')
        apk = matches[0]
        patched = tmp/'RAS.apk'
        subprocess.run(['go','run','./scripts/android-package','-input',str(apk),'-output',str(patched),'-dex',str(tmp/'classes.dex')],cwd=root,check=True)
        shutil.copy2(patched,apk)
        # NSGo names arm64 => android-arm64, x64 => android-x64 in releases.
        release = list((out/'releases').glob(f'ras-*-{target.replace("_","-")}.apk'))
        for dest in release:
            shutil.copy2(patched,dest)
            dest.with_name(dest.name+'.sha256').write_text(hashlib.sha256(dest.read_bytes()).hexdigest()+'  '+dest.name+'\n')
