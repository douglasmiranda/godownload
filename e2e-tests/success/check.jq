# Every task must run and finish without errors or warnings, and the config
# must load without diagnostics.
[ .tasks[]
  | select((.errors | length) > 0 or (.warnings | length) > 0)
  | .task_name
] as $bad
| if ($bad | length) > 0 then
    ("tasks with errors or warnings: " + ($bad | join(", ")) + "\n") | halt_error(1)
  elif (.diagnostics | length) > 0 then
    ("config diagnostics: " + (.diagnostics | join("; ")) + "\n") | halt_error(1)
  elif (.not_run | length) > 0 then
    ("tasks not run: " + (.not_run | join(", ")) + "\n") | halt_error(1)
  else
    "all \(.tasks | length) tasks succeeded without warnings"
  end
