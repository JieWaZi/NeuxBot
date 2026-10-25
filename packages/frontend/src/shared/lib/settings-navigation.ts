/** 请求唯一 Settings Scene 打开通用分组，由应用外壳处理导航和焦点。 */
export function openGeneralSettings() {
  window.dispatchEvent(new Event("ally:open-general-settings"));
}
