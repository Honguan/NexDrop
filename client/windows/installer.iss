#define MyAppName "NexDrop Desktop"
#ifndef MyAppVersion
  #define MyAppVersion "2.0.2"
#endif
#ifndef SourceDir
  #define SourceDir "build\windows\x64\runner\Release"
#endif
#ifndef OutputDir
  #define OutputDir "dist"
#endif
#ifndef MyAppId
  #define MyAppId "{{7A2A80F6-0AC8-49DC-87E9-B0F16BA0F472}"
#endif

[Setup]
AppId={#MyAppId}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
DefaultDirName={autopf}\NexDrop
DefaultGroupName=NexDrop
OutputDir={#OutputDir}
OutputBaseFilename=NexDrop-Desktop_{#MyAppVersion}_windows_x64
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\NexDrop"; Filename: "{app}\NexDrop.exe"
Name: "{autodesktop}\NexDrop"; Filename: "{app}\NexDrop.exe"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Classes\nexdrop"; ValueType: string; ValueName: ""; ValueData: "URL:NexDrop Protocol"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Classes\nexdrop"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""
Root: HKCU; Subkey: "Software\Classes\nexdrop\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: "{app}\NexDrop.exe,0"
Root: HKCU; Subkey: "Software\Classes\nexdrop\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\NexDrop.exe"" ""%1"""

[Tasks]
Name: "desktopicon"; Description: "建立桌面捷徑"; GroupDescription: "其他工作："; Flags: unchecked
