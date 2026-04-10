import tsq::functions

from Function f
where f.isAsync()
select f.getName() as "name"
