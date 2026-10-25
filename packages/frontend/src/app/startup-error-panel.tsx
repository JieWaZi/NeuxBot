import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/shared/ui/card";

export default function StartupErrorPanel({
  error,
  onRetry,
}: {
  error: string;
  onRetry?: () => void;
}) {
  const { t } = useTranslation("common");
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("startup.failed")}</CardTitle>
      </CardHeader>
      <CardContent>
        <p role="alert" className="text-destructive type-body break-words">
          {error}
        </p>
      </CardContent>
      <CardFooter>
        <Button onClick={onRetry}>{t("startup.retry")}</Button>
      </CardFooter>
    </Card>
  );
}
