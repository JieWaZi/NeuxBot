import { chatListKey } from "./chat-list-key";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { getChatQueryKey } from "./generated/@tanstack/react-query.gen";
import { updateChatProtection } from "./generated/sdk.gen";
import { withLocalErrorSurface } from "./local-error-surface";

export function useChatProtection() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async ({
      chatId,
      protected: keep,
    }: {
      chatId: string;
      wakerId: string;
      protected: boolean;
    }) =>
      (
        await updateChatProtection({
          ...withLocalErrorSurface({
            path: { chatId },
            body: { protected: keep },
          }),
          throwOnError: true,
        })
      ).data,
    onSuccess: async (_, { chatId, wakerId }) => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: getChatQueryKey({ path: { chatId } }),
        }),
        client.invalidateQueries({ queryKey: chatListKey(wakerId) }),
      ]);
    },
  });
}
