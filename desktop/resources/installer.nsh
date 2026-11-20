!macro customInstall
  ; Per-user installations can register botcli before the desktop app opens.
  ; All-users installations register it from each user's first app launch.
  ${if} $installMode == "CurrentUser"
    nsExec::ExecToLog /TIMEOUT=10000 '"$INSTDIR\resources\bin\botcli.exe" install'
    Pop $0
    ${if} $0 != "0"
      DetailPrint "botcli setup did not complete ($0). NeuxBot will retry at startup."
    ${endIf}
  ${endIf}
!macroend
