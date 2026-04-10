import tsq::jsx

from JsxAttribute attr
where attr.getName() = "dangerouslySetInnerHTML"
select attr.getName() as "name"
