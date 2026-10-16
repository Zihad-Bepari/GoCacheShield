<?php

namespace GoCacheShield\Exceptions;

use Exception;

class NotFoundException extends Exception
{
    public function __construct(string $message = "Item not found in loader")
    {
        parent::__construct($message);
    }
}
