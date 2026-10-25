import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { IMConnectionDetailDialog } from "./im-connection-detail-dialog";

export function IMConnectionDetailPage() {
  const { connectionId = "" } = useParams();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  return (
    <IMConnectionDetailDialog
      key={connectionId}
      connectionId={connectionId}
      initialBindingId={params.get("chat") || undefined}
      onClose={() => navigate("/im")}
    />
  );
}
