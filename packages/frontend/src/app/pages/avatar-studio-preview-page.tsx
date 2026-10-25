import { AllyAvatarStudio } from "@/modules/waker/components/avatar-studio/ally-avatar-studio";

export function AvatarStudioPreviewPage() {
  return (
    <div className="bg-canvas h-full min-h-0 overflow-y-auto">
      <AllyAvatarStudio />
    </div>
  );
}
