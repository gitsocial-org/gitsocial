// toast.go - The Windows sender: a toast under the app ID of a Start Menu shortcut, whose click opens the URL through the OS.
package desktop

import (
	"os"
	"path/filepath"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

const (
	windowsTitleEnv    = "GITSOCIAL_NOTIFY_TITLE"
	windowsBodyEnv     = "GITSOCIAL_NOTIFY_BODY"
	windowsURLEnv      = "GITSOCIAL_NOTIFY_URL"
	windowsAppIDEnv    = "GITSOCIAL_NOTIFY_APPID"
	windowsShortcutEnv = "GITSOCIAL_SHORTCUT_PATH"
	windowsTargetEnv   = "GITSOCIAL_SHORTCUT_TARGET"
)

// windowsAppID is the Application User Model ID that the Start Menu shortcut carries and the toast names.
const windowsAppID = "GitSocial.GitSocial"

// powershellAppID shows a toast under the name of PowerShell when the shortcut is missing.
const powershellAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

// shortcutTimeout bounds the first write of the shortcut, which compiles a small C# type.
const shortcutTimeout = 30 * time.Second

// windowsToastScript shows a toast; it reads the text and the URL from the environment because powershell joins trailing arguments into the script.
const windowsToastScript = `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] > $null
$xml = [Windows.Data.Xml.Dom.XmlDocument]::new()
$xml.LoadXml('<toast><visual><binding template="ToastGeneric"><text/><text/></binding></visual></toast>')
$text = $xml.GetElementsByTagName('text')
$text.Item(0).AppendChild($xml.CreateTextNode($env:` + windowsTitleEnv + `)) > $null
$text.Item(1).AppendChild($xml.CreateTextNode($env:` + windowsBodyEnv + `)) > $null
if ($env:` + windowsURLEnv + `) {
	$xml.DocumentElement.SetAttribute('activationType', 'protocol')
	$xml.DocumentElement.SetAttribute('launch', $env:` + windowsURLEnv + `)
}
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($env:` + windowsAppIDEnv + `).Show($toast)`

// windowsShortcutScript writes a shortcut that starts the TUI, with the System.AppUserModel.ID property, through IShellLinkW and IPropertyStore.
const windowsShortcutScript = `Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Runtime.InteropServices.ComTypes;
using System.Text;
public static class GitSocialShortcut {
	[ComImport, Guid("000214F9-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
	interface IShellLinkW {
		void GetPath([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder file, int max, IntPtr data, uint flags);
		void GetIDList(out IntPtr list);
		void SetIDList(IntPtr list);
		void GetDescription([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder name, int max);
		void SetDescription([MarshalAs(UnmanagedType.LPWStr)] string name);
		void GetWorkingDirectory([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder dir, int max);
		void SetWorkingDirectory([MarshalAs(UnmanagedType.LPWStr)] string dir);
		void GetArguments([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder args, int max);
		void SetArguments([MarshalAs(UnmanagedType.LPWStr)] string args);
		void GetHotkey(out short key);
		void SetHotkey(short key);
		void GetShowCmd(out int cmd);
		void SetShowCmd(int cmd);
		void GetIconLocation([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int max, out int index);
		void SetIconLocation([MarshalAs(UnmanagedType.LPWStr)] string path, int index);
		void SetRelativePath([MarshalAs(UnmanagedType.LPWStr)] string path, uint reserved);
		void Resolve(IntPtr window, uint flags);
		void SetPath([MarshalAs(UnmanagedType.LPWStr)] string file);
	}
	[ComImport, Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
	interface IPropertyStore {
		void GetCount(out uint count);
		void GetAt(uint index, out PropertyKey key);
		void GetValue(ref PropertyKey key, out PropVariant value);
		void SetValue(ref PropertyKey key, ref PropVariant value);
		void Commit();
	}
	[StructLayout(LayoutKind.Sequential, Pack = 4)]
	struct PropertyKey { public Guid Format; public uint ID; }
	[StructLayout(LayoutKind.Explicit, Size = 24)]
	struct PropVariant { [FieldOffset(0)] public ushort Type; [FieldOffset(8)] public IntPtr Text; }
	[ComImport, Guid("00021401-0000-0000-C000-000000000046"), ClassInterface(ClassInterfaceType.None)]
	class ShellLink {}
	public static void Write(string path, string target, string appID) {
		IShellLinkW link = (IShellLinkW)new ShellLink();
		link.SetPath(target);
		link.SetArguments("tui");
		PropertyKey key = new PropertyKey { Format = new Guid("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3"), ID = 5 };
		PropVariant value = new PropVariant { Type = 31, Text = Marshal.StringToCoTaskMemUni(appID) };
		IPropertyStore store = (IPropertyStore)link;
		store.SetValue(ref key, ref value);
		store.Commit();
		Marshal.FreeCoTaskMem(value.Text);
		((IPersistFile)link).Save(path, true);
	}
}
'@
[GitSocialShortcut]::Write($env:` + windowsShortcutEnv + `, $env:` + windowsTargetEnv + `, $env:` + windowsAppIDEnv + `)`

// windowsSend writes the Start Menu shortcut on the first use and shows a toast; without the shortcut the toast shows under PowerShell.
func windowsSend(h host, m notifications.DesktopMessage) error {
	tool := lookPath("powershell")
	if tool == "" {
		return nil
	}
	appID := powershellAppID
	if windowsShortcut(h, tool) {
		appID = windowsAppID
	}
	return run(h.timeout, tool, windowsArgs(windowsToastScript), windowsEnv(m.Title, m.Body, m.URL, appID))
}

// windowsShortcut writes GitSocial.lnk into the Start Menu when it is missing and reports whether it is there.
func windowsShortcut(h host, tool string) bool {
	if h.startMenu == "" || h.self == "" {
		return false
	}
	path := filepath.Join(h.startMenu, "GitSocial.lnk")
	if _, err := os.Stat(path); err == nil {
		return true
	}
	env := []string{windowsShortcutEnv + "=" + path, windowsTargetEnv + "=" + h.self, windowsAppIDEnv + "=" + windowsAppID}
	if err := run(shortcutTimeout, tool, windowsArgs(windowsShortcutScript), env); err != nil {
		log.Debug("windows start menu shortcut", "error", err)
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// windowsArgs returns the arguments that run a script in powershell.
func windowsArgs(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", script}
}

// windowsEnv returns the environment that carries the text, the URL and the app ID to the toast script.
func windowsEnv(title, body, url, appID string) []string {
	return []string{windowsTitleEnv + "=" + title, windowsBodyEnv + "=" + body, windowsURLEnv + "=" + url, windowsAppIDEnv + "=" + appID}
}
