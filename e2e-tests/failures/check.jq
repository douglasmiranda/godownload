# Every task must run and finish with at least one error, so one failing
# task cannot hide another that accidentally succeeded.
[.tasks[] | select((.errors | length) == 0) | .task_name] as $passed
| if ($passed | length) > 0 then
    ("tasks that did not fail: " + ($passed | join(", ")) + "\n") | halt_error(1)
  elif (.not_run | length) > 0 then
    ("tasks not run: " + (.not_run | join(", ")) + "\n") | halt_error(1)
  else
    "all \(.tasks | length) tasks failed as expected"
  end
