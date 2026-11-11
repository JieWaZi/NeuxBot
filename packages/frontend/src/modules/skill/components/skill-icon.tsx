import { ResourceCategoryIcon } from "@/shared/ui/resource-category-icon";

type Props = {
  category?: string;
  className?: string;
};

export function SkillIcon({ category, className }: Props) {
  return <ResourceCategoryIcon category={category} className={className} />;
}
