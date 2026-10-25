/** 工作台拥有草稿；文件查看器卸载和 Session 切换不清除未保存内容。 */
export type DocumentBuffer = {
  text: string;
  savedText: string;
  baseVersion?: string;
  revision: number;
};

class DocumentBuffers {
  private readonly buffers = new Map<string, DocumentBuffer>();

  get(key: string): DocumentBuffer | undefined {
    return this.buffers.get(key);
  }

  begin(key: string, text: string, baseVersion?: string): DocumentBuffer {
    const existing = this.buffers.get(key);
    if (existing) return existing;
    const buffer = { text, savedText: text, baseVersion, revision: 0 };
    this.buffers.set(key, buffer);
    return buffer;
  }

  change(key: string, text: string): DocumentBuffer {
    const previous = this.buffers.get(key);
    if (!previous) throw new Error("文件草稿尚未建立");
    const next = { ...previous, text, revision: previous.revision + 1 };
    this.buffers.set(key, next);
    return next;
  }

  saved(
    key: string,
    revision: number,
    savedText: string,
    version?: string,
  ): boolean {
    const previous = this.buffers.get(key);
    if (!previous) return true;
    if (previous.revision === revision) {
      this.buffers.delete(key);
      return true;
    }
    this.buffers.set(key, { ...previous, savedText, baseVersion: version });
    return false;
  }

  isDirty(key: string): boolean {
    const buffer = this.buffers.get(key);
    return Boolean(buffer && buffer.text !== buffer.savedText);
  }

  hasDirty(): boolean {
    for (const buffer of this.buffers.values())
      if (buffer.text !== buffer.savedText) return true;
    return false;
  }

  hasDirtyPrefix(prefix: string): boolean {
    for (const [key, buffer] of this.buffers)
      if (key.startsWith(prefix) && buffer.text !== buffer.savedText)
        return true;
    return false;
  }

  discardPrefix(prefix: string): void {
    for (const key of this.buffers.keys())
      if (key.startsWith(prefix)) this.buffers.delete(key);
  }

  discard(key: string): void {
    this.buffers.delete(key);
  }
}

export const workbenchDocumentBuffers = new DocumentBuffers();

/** Let Electron Main ask before a window close or renderer reload loses drafts. */
export function preventUnloadWithDirtyDrafts(event: BeforeUnloadEvent): void {
  if (!workbenchDocumentBuffers.hasDirty()) return;
  event.preventDefault();
  event.returnValue = false;
}
