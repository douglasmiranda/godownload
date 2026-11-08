# Every task must run, finish without errors, and produce at least one
# warning, either at runtime or as a config diagnostic.
.diagnostics as $diags
| [ .tasks[]
    | .task_name as $name
    | select(
        (.errors | length) > 0
        or ((.warnings | length) == 0
            and ([$diags[] | select(startswith("[warning] " + $name + "."))] | length) == 0)
      )
    | $name
  ] as $bad
| if ($bad | length) > 0 then
    ("tasks without exactly the expected warnings: " + ($bad | join(", ")) + "\n") | halt_error(1)
  elif (.not_run | length) > 0 then
    ("tasks not run: " + (.not_run | join(", ")) + "\n") | halt_error(1)
  else
    "all \(.tasks | length) tasks produced warnings and no errors"
  end
