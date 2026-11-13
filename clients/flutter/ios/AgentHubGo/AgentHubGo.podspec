Pod::Spec.new do |s|
  s.name = 'AgentHubGo'
  s.version = '0.1.0'
  s.summary = 'Agent Hub native Go core'
  s.homepage = 'https://localhost/agenthub'
  s.license = { :type => 'See repository license' }
  s.author = 'Agent Hub contributors'
  s.source = { :path => '.' }
  s.platform = :ios, '15.0'
  s.vendored_frameworks = 'AgentHubGo.xcframework'
  s.frameworks = 'Security', 'CoreFoundation', 'SystemConfiguration', 'CFNetwork'
  # Dart discovers these C symbols dynamically; keep them through dead stripping.
  s.user_target_xcconfig = { 'OTHER_LDFLAGS' => '$(inherited) -Wl,-u,_AgentHubCall -Wl,-u,_AgentHubFree' }
end
